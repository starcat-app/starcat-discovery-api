package config

import "testing"

func TestLoadKeepsStarHistoryDisabledWithoutGCPConfiguration(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("STAR_HISTORY_ENABLED", "false")
	t.Setenv("GCP_PROJECT_ID", "")
	t.Setenv("BIGQUERY_MAX_BYTES_BILLED", "")
	t.Setenv("STAR_HISTORY_DAILY_MAX_BYTES_BILLED", "")

	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.StarHistoryEnabled {
		t.Fatal("star history should default to disabled")
	}
}

func TestLoadRequiresBudgetOnlyWhenStarHistoryIsEnabled(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("STAR_HISTORY_ENABLED", "true")
	t.Setenv("GCP_PROJECT_ID", "starcat-project")
	t.Setenv("BIGQUERY_MAX_BYTES_BILLED", "1000")
	t.Setenv("STAR_HISTORY_DAILY_MAX_BYTES_BILLED", "500")

	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a daily budget smaller than one query")
	}

	t.Setenv("STAR_HISTORY_DAILY_MAX_BYTES_BILLED", "5000")
	config, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if config.BigQueryMaxBytesBilled != 1000 || config.StarHistoryDailyBudget != 5000 {
		t.Fatalf("unexpected budgets: %+v", config)
	}
}

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("API_KEYS", "api-key")
	t.Setenv("ADMIN_API_KEYS", "admin-key")
	t.Setenv("GITHUB_TOKENS", "github-token")
}
