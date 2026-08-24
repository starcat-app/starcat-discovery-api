package awesome

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	gh "github.com/starcat-app/starcat-discovery-api/internal/github"
	"github.com/starcat-app/starcat-discovery-api/internal/model"
	"github.com/starcat-app/starcat-discovery-api/internal/store"
)

var sourceIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Store is the persistence boundary used by Awesome Service.
type Store interface {
	CreateAwesomeSource(context.Context, model.AwesomeSource) (model.AwesomeSource, error)
	UpdateAwesomeSource(context.Context, model.AwesomeSource, int) (model.AwesomeSource, error)
	GetAwesomeSource(context.Context, string) (model.AwesomeSource, error)
	ListAwesomeSources(context.Context) ([]model.AwesomeSource, error)
	ListPublishedAwesomeSources(context.Context) ([]model.AwesomeSource, error)
	SetAwesomeSourceStatus(context.Context, string, model.AwesomeSourceStatus) (model.AwesomeSource, error)
	StartAwesomeSyncRun(context.Context, model.AwesomeSyncRun) (model.AwesomeSyncRun, error)
	GetActiveAwesomeSyncRun(context.Context, string) (model.AwesomeSyncRun, error)
	FinishAwesomeSyncRun(context.Context, model.AwesomeSyncRun) error
	ListAwesomeSyncRuns(context.Context, string, int) ([]model.AwesomeSyncRun, error)
	UpsertAwesomeRepositories(context.Context, []model.Repository) error
	ReplaceAwesomeSnapshot(context.Context, string, string, string, string, []model.Repository, []model.AwesomeEntry, model.AwesomeSyncRun) error
	ListPublishedAwesomeEntries(context.Context, string) ([]model.AwesomeEntry, error)
}

// GitHubClient is deliberately limited to public repository and README facts.
type GitHubClient interface {
	GetRepository(context.Context, string) (gh.Repository, error)
	GetREADME(context.Context, string) (gh.README, error)
}

// ServiceError exposes a stable Awesome code without leaking upstream response bodies.
type ServiceError struct {
	Status  int
	Code    string
	Message string
}

func (e *ServiceError) Error() string { return e.Message }

// Service coordinates managed source validation, state transitions and transactional sync.
type Service struct {
	store  Store
	github GitHubClient
	now    func() time.Time
}

// NewService creates the managed Awesome application service.
func NewService(store Store, github GitHubClient) *Service {
	return &Service{store: store, github: github, now: func() time.Time { return time.Now().UTC() }}
}

// CreateSource validates the public GitHub source before persisting a draft.
func (s *Service) CreateSource(ctx context.Context, source model.AwesomeSource) (model.AwesomeSource, error) {
	if err := validateSourceFields(source, true); err != nil {
		return model.AwesomeSource{}, err
	}
	canonical, err := NormalizeSourceInput(source.RepoFullName)
	if err != nil {
		return model.AwesomeSource{}, invalidSource(err.Error())
	}
	repo, err := s.validateSourceRepository(ctx, canonical)
	if err != nil {
		return model.AwesomeSource{}, err
	}
	if _, err := s.github.GetREADME(ctx, repo.FullName); err != nil {
		return model.AwesomeSource{}, mapGitHubError(err, "来源仓库 README 不可读取")
	}
	source.RepoFullName = repo.FullName
	created, err := s.store.CreateAwesomeSource(ctx, source)
	return created, mapStoreError(err)
}

// UpdateSource applies optimistic concurrency and prevents a published ID from being rebound.
func (s *Service) UpdateSource(ctx context.Context, source model.AwesomeSource, expectedRevision int) (model.AwesomeSource, error) {
	if expectedRevision <= 0 {
		return model.AwesomeSource{}, invalidSource("revision must be positive")
	}
	if err := validateSourceFields(source, false); err != nil {
		return model.AwesomeSource{}, err
	}
	current, err := s.store.GetAwesomeSource(ctx, source.ID)
	if err != nil {
		return model.AwesomeSource{}, mapStoreError(err)
	}
	canonical, err := NormalizeSourceInput(source.RepoFullName)
	if err != nil {
		return model.AwesomeSource{}, invalidSource(err.Error())
	}
	if !strings.EqualFold(canonical, current.RepoFullName) {
		if current.Status != model.AwesomeSourceDraft {
			return model.AwesomeSource{}, conflict("AWESOME_SOURCE_CONFLICT", "已同步或发布的来源不能更换仓库")
		}
		repo, validateErr := s.validateSourceRepository(ctx, canonical)
		if validateErr != nil {
			return model.AwesomeSource{}, validateErr
		}
		if _, readmeErr := s.github.GetREADME(ctx, repo.FullName); readmeErr != nil {
			return model.AwesomeSource{}, mapGitHubError(readmeErr, "来源仓库 README 不可读取")
		}
		source.RepoFullName = repo.FullName
	} else {
		source.RepoFullName = current.RepoFullName
	}
	updated, err := s.store.UpdateAwesomeSource(ctx, source, expectedRevision)
	return updated, mapStoreError(err)
}

func (s *Service) ListSources(ctx context.Context) ([]model.AwesomeSource, error) {
	return s.store.ListAwesomeSources(ctx)
}

func (s *Service) ListPublishedSources(ctx context.Context) ([]model.AwesomeSource, error) {
	return s.store.ListPublishedAwesomeSources(ctx)
}

// SyncPublishedSources refreshes only public managed sources during the regular discovery cron.
func (s *Service) SyncPublishedSources(ctx context.Context) error {
	sources, err := s.store.ListPublishedAwesomeSources(ctx)
	if err != nil {
		return err
	}
	var syncErrors []error
	for _, source := range sources {
		if _, syncErr := s.SyncSource(ctx, source.ID, "scheduler"); syncErr != nil {
			syncErrors = append(syncErrors, fmt.Errorf("%s: %w", source.ID, syncErr))
		}
	}
	return errors.Join(syncErrors...)
}

func (s *Service) PublishedEntries(ctx context.Context, sourceID string) (model.AwesomeEntriesSnapshot, error) {
	source, err := s.store.GetAwesomeSource(ctx, sourceID)
	if err != nil || source.Status != model.AwesomeSourcePublished {
		return model.AwesomeEntriesSnapshot{}, notFound()
	}
	entries, err := s.store.ListPublishedAwesomeEntries(ctx, sourceID)
	if err != nil {
		return model.AwesomeEntriesSnapshot{}, err
	}
	snapshotUpdatedAt := source.UpdatedAt
	if source.LastSyncedAt != nil {
		snapshotUpdatedAt = *source.LastSyncedAt
	}
	return model.AwesomeEntriesSnapshot{
		Source:  model.AwesomeEntriesSource{ID: source.ID, DisplayName: source.DisplayName, UpdatedAt: snapshotUpdatedAt},
		Entries: entries,
	}, nil
}

// SyncSource persists an active run before network work and never clears the previous snapshot on failure.
func (s *Service) SyncSource(ctx context.Context, sourceID, trigger string) (model.AwesomeSyncRun, error) {
	if trigger != "scheduler" {
		trigger = "manual"
	}
	source, err := s.store.GetAwesomeSource(ctx, sourceID)
	if err != nil {
		return model.AwesomeSyncRun{}, mapStoreError(err)
	}
	run := model.AwesomeSyncRun{
		ID: newRunID(), SourceID: sourceID, Status: "running", Trigger: trigger, StartedAt: s.now(),
	}
	run, err = s.store.StartAwesomeSyncRun(ctx, run)
	if errors.Is(err, store.ErrAwesomeSyncInProgress) {
		active, activeErr := s.store.GetActiveAwesomeSyncRun(ctx, sourceID)
		if activeErr != nil {
			return model.AwesomeSyncRun{}, activeErr
		}
		return active, nil
	}
	if err != nil {
		return model.AwesomeSyncRun{}, err
	}

	result, syncErr := s.buildSnapshot(ctx, source, run)
	if syncErr != nil {
		run.Status = "failed"
		run.ErrorCode, run.ErrorMessage = syncFailure(syncErr)
		_ = s.store.FinishAwesomeSyncRun(context.Background(), run)
		return run, syncErr
	}
	return result, nil
}

func (s *Service) buildSnapshot(ctx context.Context, source model.AwesomeSource, run model.AwesomeSyncRun) (model.AwesomeSyncRun, error) {
	sourceRepo, err := s.validateSourceRepository(ctx, source.RepoFullName)
	if err != nil {
		return run, err
	}
	// 来源仓库本身不属于 README 条目，但卡片仍需展示它的实时 GitHub 元数据。
	// 每轮同步都先刷新共享 repos 主表，这样 README SHA 未变化时 Stars 也不会停滞。
	if err := s.store.UpsertAwesomeRepositories(ctx, []model.Repository{repositoryModel(sourceRepo, s.now())}); err != nil {
		return run, err
	}
	readme, err := s.github.GetREADME(ctx, sourceRepo.FullName)
	if err != nil {
		return run, mapGitHubError(err, "读取来源 README 失败")
	}
	if readme.SHA != "" && readme.SHA == source.LastSuccessfulSHA {
		run.Status = "succeeded"
		run.ReadmeSHA = readme.SHA
		run.GitHubCount = source.GitHubRepoCount
		run.ExternalCount = source.ExternalEntryCount
		if err := s.store.FinishAwesomeSyncRun(ctx, run); err != nil {
			return run, err
		}
		return run, nil
	}
	parsed, err := ParseREADME(readme.Content, sourceRepo.FullName, readme.HTMLURL, readme.SHA)
	if err != nil {
		return run, &ServiceError{Status: http.StatusUnprocessableEntity, Code: "AWESOME_README_UNSUPPORTED", Message: err.Error()}
	}
	repos := make([]model.Repository, 0)
	entries := make([]model.AwesomeEntry, 0, len(parsed.Entries))
	seenRepoIDs := make(map[int64]struct{})
	for _, entry := range parsed.Entries {
		if entry.TargetType != "github_repo" {
			entries = append(entries, entry)
			continue
		}
		candidate := strings.TrimPrefix(entry.TargetKey, "github_name:")
		repo, repoErr := s.github.GetRepository(ctx, candidate)
		if repoErr != nil {
			var apiErr *gh.APIError
			if errors.As(repoErr, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				parsed.InvalidCount++
				continue
			}
			return run, mapGitHubError(repoErr, "核验 README 中的 GitHub 仓库失败")
		}
		if repo.Private || repo.ID <= 0 {
			parsed.InvalidCount++
			continue
		}
		if _, duplicate := seenRepoIDs[repo.ID]; duplicate {
			parsed.DuplicateCount++
			continue
		}
		seenRepoIDs[repo.ID] = struct{}{}
		repoID := repo.ID
		entry.TargetKey = fmt.Sprintf("github:%d", repo.ID)
		entry.GhRepoID = &repoID
		entry.RawURL = "https://github.com/" + repo.FullName
		entries = append(entries, entry)
		repos = append(repos, repositoryModel(repo, s.now()))
	}
	if len(repos) == 0 {
		return run, &ServiceError{Status: http.StatusUnprocessableEntity, Code: "AWESOME_README_UNSUPPORTED", Message: "README 没有可发布的 GitHub Repo"}
	}
	run.InvalidCount = parsed.InvalidCount
	run.DuplicateCount = parsed.DuplicateCount
	if err := s.store.ReplaceAwesomeSnapshot(ctx, source.ID, sourceRepo.DefaultBranch, readme.Path, readme.SHA, repos, entries, run); err != nil {
		return run, err
	}
	run.Status = "succeeded"
	run.ReadmeSHA = readme.SHA
	for _, entry := range entries {
		if entry.TargetType == "github_repo" {
			run.GitHubCount++
		} else {
			run.ExternalCount++
		}
	}
	return run, nil
}

func (s *Service) PublishSource(ctx context.Context, sourceID string) (model.AwesomeSource, error) {
	source, err := s.store.GetAwesomeSource(ctx, sourceID)
	if err != nil {
		return model.AwesomeSource{}, mapStoreError(err)
	}
	if source.LastSuccessfulSHA == "" || source.GitHubRepoCount == 0 ||
		(source.Status != model.AwesomeSourceReady && source.Status != model.AwesomeSourceArchived && source.Status != model.AwesomeSourcePublished) {
		return model.AwesomeSource{}, conflict("AWESOME_SOURCE_NOT_READY", "来源尚未通过成功同步")
	}
	updated, err := s.store.SetAwesomeSourceStatus(ctx, sourceID, model.AwesomeSourcePublished)
	return updated, mapStoreError(err)
}

func (s *Service) ArchiveSource(ctx context.Context, sourceID string) (model.AwesomeSource, error) {
	updated, err := s.store.SetAwesomeSourceStatus(ctx, sourceID, model.AwesomeSourceArchived)
	return updated, mapStoreError(err)
}

func (s *Service) SyncRuns(ctx context.Context, sourceID string) ([]model.AwesomeSyncRun, error) {
	if _, err := s.store.GetAwesomeSource(ctx, sourceID); err != nil {
		return nil, mapStoreError(err)
	}
	return s.store.ListAwesomeSyncRuns(ctx, sourceID, 20)
}

func (s *Service) validateSourceRepository(ctx context.Context, fullName string) (gh.Repository, error) {
	repo, err := s.github.GetRepository(ctx, fullName)
	if err != nil {
		return gh.Repository{}, mapGitHubError(err, "来源仓库不可读取")
	}
	if repo.Private || repo.Archived || repo.ID <= 0 || repo.FullName == "" || repo.DefaultBranch == "" {
		return gh.Repository{}, invalidSource("来源必须是公开、未归档且具有默认分支的 GitHub 仓库")
	}
	return repo, nil
}

func validateSourceFields(source model.AwesomeSource, requireID bool) error {
	if requireID && !sourceIDPattern.MatchString(source.ID) {
		return invalidSource("id must be a kebab-case stable key")
	}
	if source.ID == "" || strings.TrimSpace(source.DisplayName) == "" || strings.TrimSpace(source.RepoFullName) == "" {
		return invalidSource("id, repo_full_name and display_name are required")
	}
	if source.ImageURL != "" {
		parsed, err := url.Parse(source.ImageURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return invalidSource("image_url must be an absolute https URL")
		}
	}
	return nil
}

func repositoryModel(repo gh.Repository, now time.Time) model.Repository {
	return model.Repository{
		GhRepoID: repo.ID, Owner: repo.Owner.Login, Name: repo.Name, FullName: repo.FullName,
		Description: repo.Description, Language: repo.Language, Stars: repo.Stargazers,
		Forks: repo.Forks, Watchers: repo.Watchers, Subscribers: repo.Subscribers,
		OpenIssues: repo.OpenIssues, OwnerAvatar: repo.Owner.AvatarURL, DefaultBranch: repo.DefaultBranch,
		UpdatedAt:  parseAwesomeTimePtr(repo.UpdatedAt),
		IsArchived: repo.Archived, IsFork: repo.Fork, IndexedAt: now, EnrichedAt: &now,
	}
}

// GitHub 时间缺失或格式异常时保留 nil，让 API 省略 updated_at；不能用同步时间冒充
// 仓库更新时间，否则客户端“最近更新”排序会产生误导。
func parseAwesomeTimePtr(raw string) *time.Time {
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return &value
}

func newRunID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return fmt.Sprintf("run-%d", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(buffer)
}

func invalidSource(message string) error {
	return &ServiceError{Status: http.StatusBadRequest, Code: "AWESOME_SOURCE_INVALID", Message: message}
}

func conflict(code, message string) error {
	return &ServiceError{Status: http.StatusConflict, Code: code, Message: message}
}

func notFound() error {
	return &ServiceError{Status: http.StatusNotFound, Code: "AWESOME_SOURCE_NOT_FOUND", Message: "Awesome 来源不存在"}
}

func mapStoreError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, store.ErrAwesomeSourceNotFound):
		return notFound()
	case errors.Is(err, store.ErrAwesomeSourceRevisionConflict):
		return conflict("AWESOME_SOURCE_CONFLICT", "来源已被其他编辑更新，请刷新后重试")
	default:
		lower := strings.ToLower(err.Error())
		if strings.Contains(lower, "unique constraint failed") {
			return conflict("AWESOME_SOURCE_CONFLICT", "来源 ID 或 GitHub 仓库已存在")
		}
		return err
	}
}

func mapGitHubError(err error, fallback string) error {
	var apiErr *gh.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusNotFound:
			return invalidSource(fallback)
		case http.StatusForbidden, http.StatusTooManyRequests:
			return &ServiceError{Status: http.StatusTooManyRequests, Code: "GITHUB_RATE_LIMITED", Message: "GitHub API 配额不足，请稍后重试"}
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return &ServiceError{Status: http.StatusServiceUnavailable, Code: "AWESOME_SYNC_UNAVAILABLE", Message: "GitHub 暂时不可用，请稍后重试"}
		}
	}
	return &ServiceError{Status: http.StatusServiceUnavailable, Code: "AWESOME_SYNC_UNAVAILABLE", Message: fallback}
}

func syncFailure(err error) (string, string) {
	code := "AWESOME_SYNC_UNAVAILABLE"
	message := "Awesome 来源同步失败"
	var serviceErr *ServiceError
	if errors.As(err, &serviceErr) {
		code = serviceErr.Code
		message = serviceErr.Message
	}
	if len(message) > 512 {
		message = message[:512]
	}
	return code, message
}
