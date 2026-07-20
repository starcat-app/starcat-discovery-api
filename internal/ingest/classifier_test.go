package ingest

import (
	"testing"

	"github.com/starcat-app/starcat-discovery-api/internal/github"
)

func TestClassifyTopics(t *testing.T) {
	repo := github.Repository{
		Name:        "private-llm-agent",
		Description: "Local AI agent with encryption",
		Topics:      []string{"llm", "privacy"},
	}
	got := ClassifyTopics(repo)
	if !contains(got, "ai") || !contains(got, "privacy") {
		t.Fatalf("expected ai and privacy, got %v", got)
	}
}

func TestDetectPlatformsFromAssets(t *testing.T) {
	repo := github.Repository{Language: "TypeScript"}
	releases := []github.Release{{
		Assets: []github.Asset{
			{Name: "app-aarch64-apple-darwin.dmg"},
			{Name: "app-x86_64-unknown-linux.AppImage"},
			{Name: "app-win64.exe"},
		},
	}}
	got := DetectPlatforms(repo, releases)
	for _, want := range []string{"macos", "linux", "windows"} {
		if !contains(got, want) {
			t.Fatalf("expected %s in %v", want, got)
		}
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
