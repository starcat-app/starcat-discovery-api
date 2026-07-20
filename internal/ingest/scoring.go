package ingest

import (
	"math"
	"time"

	"github.com/starcat-app/starcat-discovery-api/internal/github"
)

// Scores 是写入 repos 表的预计算分数。
type Scores struct {
	Popularity float64
	Release    float64
	Discovery  float64
	Trending   float64
	Search     float64
}

// ComputeScores 计算首期静态分数。
//
// 分数归一到 0-1，后续有每日快照后可把 growth/momentum 加进来。
func ComputeScores(repo github.Repository, releases []github.Release, now time.Time) Scores {
	stars := logNorm(repo.Stargazers, 200000)
	forks := logNorm(repo.Forks, 50000)
	activity := recencyScore(parseGitHubTime(repo.PushedAt), now, 365)
	downloads := logNorm(totalDownloads(releases), 200000)
	release := latestStableReleaseScore(releases, now)

	popularity := clamp01(0.50*stars + 0.18*forks + 0.17*downloads + 0.15*activity)
	releaseScore := clamp01(0.55*release + 0.20*downloads + 0.15*stars + 0.10*activity)
	discovery := clamp01(0.35*activity + 0.25*release + 0.20*stars + 0.20*downloads)
	trending := clamp01(0.45*activity + 0.30*release + 0.25*stars)
	search := clamp01(0.65*stars + 0.35*activity)

	if repo.Archived {
		popularity *= 0.2
		releaseScore *= 0.2
		discovery *= 0.2
		trending *= 0.2
	}
	if repo.Fork {
		releaseScore *= 0.4
		discovery *= 0.7
	}

	return Scores{
		Popularity: round4(popularity),
		Release:    round4(releaseScore),
		Discovery:  round4(discovery),
		Trending:   round4(trending),
		Search:     round4(search),
	}
}

func totalDownloads(releases []github.Release) int {
	total := 0
	for _, release := range releases {
		for _, asset := range release.Assets {
			total += asset.DownloadCount
		}
	}
	return total
}

func latestStableReleaseScore(releases []github.Release, now time.Time) float64 {
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		publishedAt := parseGitHubTime(release.PublishedAt)
		return recencyScore(publishedAt, now, 180)
	}
	return 0
}

func parseGitHubTime(raw string) time.Time {
	value, _ := time.Parse(time.RFC3339, raw)
	return value
}

func recencyScore(value time.Time, now time.Time, maxAgeDays float64) float64 {
	if value.IsZero() {
		return 0
	}
	age := now.Sub(value).Hours() / 24
	if age <= 0 {
		return 1
	}
	return clamp01(1 - age/maxAgeDays)
}

func logNorm(value int, max int) float64 {
	if value <= 0 || max <= 0 {
		return 0
	}
	return clamp01(math.Log1p(float64(value)) / math.Log1p(float64(max)))
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func round4(value float64) float64 {
	return math.Round(value*10000) / 10000
}
