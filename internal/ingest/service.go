package ingest

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/dong4j/starcat-discovery-api/internal/github"
	"github.com/dong4j/starcat-discovery-api/internal/model"
	"github.com/dong4j/starcat-discovery-api/internal/store"
)

const (
	categoryMostPopular = "most-popular"
	categoryNewReleases = "new-releases"
	categoryTrending    = "trending"
)

// GitHubClient 是 ingest 依赖的 GitHub 最小接口，便于测试替换。
type GitHubClient interface {
	SearchRepositories(ctx context.Context, query string, perPage int) ([]github.Repository, error)
	GetRepository(ctx context.Context, fullName string) (github.Repository, error)
	ListReleases(ctx context.Context, fullName string, perPage int) ([]github.Release, error)
}

// Store 是 ingest 依赖的 store 最小接口。
type Store interface {
	UpsertRepo(ctx context.Context, repo model.Repository) error
	UpsertRelease(ctx context.Context, release model.Release) error
	RecordDailySnapshot(ctx context.Context, snapshot model.DailySnapshot) error
	ReplaceCategoryRanking(ctx context.Context, category, bucket string, entries []model.RankingEntry) error
	ReplaceTopicRanking(ctx context.Context, topic, platform string, entries []model.RankingEntry) error
	TopRankingEntries(ctx context.Context, scoreColumn string, filters store.QueryFilters, limit int) ([]model.RankingEntry, error)
	ListLanguages(ctx context.Context) ([]model.LanguageStat, error)
	PruneReposNotIn(ctx context.Context, keepIDs []int64) (int, error)
	StartSyncRun(ctx context.Context, mode string) (int64, time.Time, error)
	FinishSyncRun(ctx context.Context, runID int64, status string, reposSeen, reposUpserted int, errorMessage string) (time.Time, error)
}

// Service 编排 discovery 同步流程。
type Service struct {
	store       Store
	github      GitHubClient
	searchLimit int
	now         func() time.Time
}

// NewService 创建同步服务。
func NewService(store Store, github GitHubClient, searchLimit int) *Service {
	if searchLimit <= 0 {
		searchLimit = 30
	}
	if searchLimit > 50 {
		searchLimit = 50
	}
	return &Service{
		store:       store,
		github:      github,
		searchLimit: searchLimit,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

// Sync 执行一次同步并刷新排名。
func (s *Service) Sync(ctx context.Context, mode string) (model.SyncResult, error) {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		mode = "manual"
	}
	runID, startedAt, err := s.store.StartSyncRun(ctx, mode)
	if err != nil {
		return model.SyncResult{}, err
	}

	result := model.SyncResult{
		RunID:     runID,
		Mode:      mode,
		Status:    "running",
		StartedAt: startedAt.Format(time.RFC3339),
	}
	finish := func(status string, syncErr error) (model.SyncResult, error) {
		message := ""
		if syncErr != nil {
			message = syncErr.Error()
		}
		finishedAt, finishErr := s.store.FinishSyncRun(ctx, runID, status, result.ReposSeen, result.ReposUpserted, message)
		result.Status = status
		result.FinishedAt = finishedAt.Format(time.RFC3339)
		result.ErrorMessage = message
		if finishErr != nil {
			return result, finishErr
		}
		return result, syncErr
	}

	candidateIDs, err := s.syncSeeds(ctx, &result)
	if err != nil {
		return finish("failed", err)
	}
	if isFullSyncMode(mode) {
		if len(candidateIDs) == 0 {
			return finish("failed", fmt.Errorf("full sync produced no candidates; skip prune"))
		}
		pruned, err := s.store.PruneReposNotIn(ctx, candidateIDs)
		if err != nil {
			return finish("failed", err)
		}
		result.ReposPruned = pruned
	}
	if err := s.refreshRankings(ctx); err != nil {
		return finish("failed", err)
	}
	return finish("success", nil)
}

func (s *Service) syncSeeds(ctx context.Context, result *model.SyncResult) ([]int64, error) {
	seen := map[int64]bool{}
	candidateIDs := make([]int64, 0)
	for _, seed := range defaultSeeds() {
		repos, err := s.github.SearchRepositories(ctx, seed.Query, s.searchLimit)
		if err != nil {
			return nil, fmt.Errorf("search seed %s: %w", seed.Query, err)
		}
		for _, candidate := range repos {
			if candidate.ID > 0 && seen[candidate.ID] {
				continue
			}
			if candidate.ID > 0 {
				seen[candidate.ID] = true
				candidateIDs = append(candidateIDs, candidate.ID)
			}
			result.ReposSeen++
			repo, err := s.github.GetRepository(ctx, candidate.FullName)
			if err != nil {
				log.Printf("[ingest] get repo %s failed: %v", candidate.FullName, err)
				continue
			}
			releases, err := s.github.ListReleases(ctx, repo.FullName, 5)
			if err != nil {
				log.Printf("[ingest] list releases %s failed: %v", repo.FullName, err)
				releases = nil
			}
			if err := s.upsertRepo(ctx, repo, releases, seed.Topic); err != nil {
				log.Printf("[ingest] upsert repo %s failed: %v", repo.FullName, err)
				continue
			}
			result.ReposUpserted++
		}
	}
	return candidateIDs, nil
}

func isFullSyncMode(mode string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))
	return mode == "full" || strings.HasSuffix(mode, "-full") || strings.Contains(mode, "full-sync")
}

func (s *Service) upsertRepo(ctx context.Context, repo github.Repository, releases []github.Release, seedTopic string) error {
	now := s.now()
	topics := ClassifyTopics(repo)
	if seedTopic != "" && !containsString(topics, seedTopic) {
		topics = append(topics, seedTopic)
		sort.Strings(topics)
	}
	platforms := DetectPlatforms(repo, releases)
	scores := ComputeScores(repo, releases, now)
	latestRelease := latestStableRelease(releases)
	downloadCount := totalDownloads(releases)

	modelRepo := model.Repository{
		GhRepoID:             repo.ID,
		Owner:                repo.Owner.Login,
		Name:                 repo.Name,
		FullName:             repo.FullName,
		Description:          repo.Description,
		Homepage:             repo.Homepage,
		Language:             repo.Language,
		Stars:                repo.Stargazers,
		Forks:                repo.Forks,
		Watchers:             repo.Watchers,
		Subscribers:          repo.Subscribers,
		OpenIssues:           repo.OpenIssues,
		OwnerAvatar:          repo.Owner.AvatarURL,
		DefaultBranch:        repo.DefaultBranch,
		LicenseSpdx:          licenseSPDX(repo),
		Topics:               topics,
		Platforms:            platforms,
		PushedAt:             parseTimePtr(repo.PushedAt),
		UpdatedAt:            parseTimePtr(repo.UpdatedAt),
		CreatedAt:            parseTimePtr(repo.CreatedAt),
		IsArchived:           repo.Archived,
		IsFork:               repo.Fork,
		ReleaseDownloadCount: downloadCount,
		TrendingScore:        scores.Trending,
		PopularityScore:      scores.Popularity,
		ReleaseScore:         scores.Release,
		DiscoveryScore:       scores.Discovery,
		SearchScore:          scores.Search,
		IndexedAt:            now,
		EnrichedAt:           &now,
	}
	if latestRelease != nil {
		modelRepo.LatestReleaseTag = latestRelease.TagName
		modelRepo.LatestReleaseAt = parseTimePtr(latestRelease.PublishedAt)
		modelRepo.LatestReleaseURL = latestRelease.HTMLURL
	}
	if err := s.store.UpsertRepo(ctx, modelRepo); err != nil {
		return err
	}
	for _, release := range releases {
		publishedAt := parseTimePtr(release.PublishedAt)
		if publishedAt == nil {
			continue
		}
		if err := s.store.UpsertRelease(ctx, model.Release{
			GhRepoID:      repo.ID,
			TagName:       release.TagName,
			Name:          release.Name,
			HTMLURL:       release.HTMLURL,
			PublishedAt:   *publishedAt,
			Draft:         release.Draft,
			Prerelease:    release.Prerelease,
			DownloadCount: releaseDownloadCount(release),
			Assets:        convertAssets(release.Assets),
			IndexedAt:     now,
		}); err != nil {
			return err
		}
	}
	return s.store.RecordDailySnapshot(ctx, model.DailySnapshot{
		Date:                 now.Format("2006-01-02"),
		GhRepoID:             repo.ID,
		Stars:                repo.Stargazers,
		Forks:                repo.Forks,
		Watchers:             repo.Watchers,
		ReleaseDownloadCount: downloadCount,
		CapturedAt:           now,
	})
}

func (s *Service) refreshRankings(ctx context.Context) error {
	categories := []struct {
		category    string
		scoreColumn string
	}{
		{category: categoryMostPopular, scoreColumn: "popularity_score"},
		{category: categoryNewReleases, scoreColumn: "release_score"},
	}
	for _, item := range categories {
		baseFilters := store.QueryFilters{Category: item.category}
		if item.category == categoryNewReleases {
			baseFilters.MinReleaseAt = s.now().AddDate(0, 0, -180).Format(time.RFC3339)
		}
		if err := s.replaceCategoryBucket(ctx, item.category, item.scoreColumn, model.AllBucket, baseFilters); err != nil {
			return err
		}
		languages, err := s.store.ListLanguages(ctx)
		if err != nil {
			return err
		}
		for _, language := range languages {
			bucket := "language:" + language.Key
			filters := baseFilters
			filters.Language = language.Key
			if err := s.replaceCategoryBucket(ctx, item.category, item.scoreColumn, bucket, filters); err != nil {
				return err
			}
		}
		for _, platform := range model.DefaultPlatforms {
			bucket := "platform:" + platform.Code
			filters := baseFilters
			filters.Platform = platform.Code
			if err := s.replaceCategoryBucket(ctx, item.category, item.scoreColumn, bucket, filters); err != nil {
				return err
			}
		}
	}

	for _, topic := range model.DefaultTopics {
		if err := s.replaceTopicBucket(ctx, topic.Code, model.AllBucket, store.QueryFilters{Topic: topic.Code}); err != nil {
			return err
		}
		for _, platform := range model.DefaultPlatforms {
			if err := s.replaceTopicBucket(ctx, topic.Code, platform.Code, store.QueryFilters{Topic: topic.Code, Platform: platform.Code}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) replaceCategoryBucket(ctx context.Context, category, scoreColumn, bucket string, filters store.QueryFilters) error {
	entries, err := s.store.TopRankingEntries(ctx, scoreColumn, filters, 500)
	if err != nil {
		return err
	}
	return s.store.ReplaceCategoryRanking(ctx, category, bucket, entries)
}

func (s *Service) replaceTopicBucket(ctx context.Context, topic, platform string, filters store.QueryFilters) error {
	entries, err := s.store.TopRankingEntries(ctx, "discovery_score", filters, 500)
	if err != nil {
		return err
	}
	return s.store.ReplaceTopicRanking(ctx, topic, platform, entries)
}

type seed struct {
	Topic string
	Query string
}

func defaultSeeds() []seed {
	return []seed{
		{Topic: "ai", Query: "topic:llm stars:>100 archived:false"},
		{Topic: "ai", Query: "topic:machine-learning stars:>500 archived:false"},
		{Topic: "privacy", Query: "topic:privacy stars:>100 archived:false"},
		{Topic: "networking", Query: "topic:networking stars:>100 archived:false"},
		{Topic: "media", Query: "topic:media stars:>100 archived:false"},
		{Topic: "social", Query: "topic:social stars:>100 archived:false"},
		{Topic: "reading", Query: "topic:rss stars:>100 archived:false"},
		{Topic: "tools", Query: "topic:cli stars:>100 archived:false"},
	}
}

func latestStableRelease(releases []github.Release) *github.Release {
	for i := range releases {
		if !releases[i].Draft && !releases[i].Prerelease {
			return &releases[i]
		}
	}
	return nil
}

func releaseDownloadCount(release github.Release) int {
	total := 0
	for _, asset := range release.Assets {
		total += asset.DownloadCount
	}
	return total
}

func convertAssets(assets []github.Asset) []model.ReleaseAsset {
	result := make([]model.ReleaseAsset, 0, len(assets))
	for _, asset := range assets {
		result = append(result, model.ReleaseAsset{
			Name:               asset.Name,
			BrowserDownloadURL: asset.BrowserDownloadURL,
			DownloadCount:      asset.DownloadCount,
		})
	}
	return result
}

func parseTimePtr(raw string) *time.Time {
	value := parseGitHubTime(raw)
	if value.IsZero() {
		return nil
	}
	return &value
}

func licenseSPDX(repo github.Repository) string {
	if repo.License == nil {
		return ""
	}
	return repo.License.SPDXID
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
