package starhistory

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGHArchiveProviderUsesNamedParametersAndBudget(t *testing.T) {
	runner := &recordingBigQueryRunner{
		results: []BigQueryResult{
			{TotalBytesProcessed: 100},
			{
				Rows: []DailyWatchEvent{
					{Date: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC), Count: 3},
					{Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), Count: 2},
				},
				TotalBytesProcessed: 120,
			},
		},
	}
	provider, err := NewGHArchiveBigQueryProvider("starcat-project", runner)
	if err != nil {
		t.Fatalf("NewGHArchiveBigQueryProvider() error = %v", err)
	}

	rows, err := provider.DailyWatchEvents(context.Background(), HistoryEventRequest{
		RepoID:             41881900,
		StartDate:          time.Date(2026, 7, 1, 8, 0, 0, 0, time.FixedZone("CST", 8*3600)),
		EndDate:            time.Date(2026, 7, 3, 8, 0, 0, 0, time.FixedZone("CST", 8*3600)),
		MaximumBytesBilled: 1024,
	})
	if err != nil {
		t.Fatalf("DailyWatchEvents() error = %v", err)
	}
	if len(runner.requests) != 2 || !runner.requests[0].DryRun || runner.requests[1].DryRun {
		t.Fatalf("expected dry run then query, got %+v", runner.requests)
	}
	for _, request := range runner.requests {
		if request.MaximumBytesBilled != 1024 {
			t.Fatalf("maximum bytes = %d", request.MaximumBytesBilled)
		}
		if strings.Contains(request.SQL, "41881900") {
			t.Fatal("repo ID was interpolated into SQL")
		}
		if !strings.Contains(request.SQL, "repo.id = @repo_id") ||
			!strings.Contains(request.SQL, "_TABLE_SUFFIX BETWEEN @start_suffix AND @end_suffix") {
			t.Fatalf("query lost required boundaries: %s", request.SQL)
		}
		if got := parameterMap(request.Parameters); got["repo_id"] != "41881900" ||
			got["start_suffix"] != "260701" || got["end_suffix"] != "260703" {
			t.Fatalf("unexpected parameters: %+v", got)
		}
	}
	if len(rows) != 2 || !rows[0].Date.Before(rows[1].Date) {
		t.Fatalf("rows were not sorted: %+v", rows)
	}
}

func TestGHArchiveProviderRejectsMissingBudgetWithoutCallingRunner(t *testing.T) {
	runner := &recordingBigQueryRunner{}
	provider, err := NewGHArchiveBigQueryProvider("starcat-project", runner)
	if err != nil {
		t.Fatalf("NewGHArchiveBigQueryProvider() error = %v", err)
	}
	_, err = provider.DailyWatchEvents(context.Background(), HistoryEventRequest{
		RepoID:    1,
		StartDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
	})
	if err == nil || !strings.Contains(err.Error(), "BIGQUERY_MAX_BYTES_BILLED") {
		t.Fatalf("missing budget error = %v", err)
	}
	if len(runner.requests) != 0 {
		t.Fatalf("runner called without budget: %+v", runner.requests)
	}
}

func TestGHArchiveProviderStopsWhenDryRunExceedsBudget(t *testing.T) {
	runner := &recordingBigQueryRunner{
		results: []BigQueryResult{{TotalBytesProcessed: 2048}},
	}
	provider, err := NewGHArchiveBigQueryProvider("starcat-project", runner)
	if err != nil {
		t.Fatalf("NewGHArchiveBigQueryProvider() error = %v", err)
	}
	_, err = provider.DailyWatchEvents(context.Background(), HistoryEventRequest{
		RepoID:             1,
		StartDate:          time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		EndDate:            time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		MaximumBytesBilled: 1024,
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds budget") {
		t.Fatalf("dry run budget error = %v", err)
	}
	if len(runner.requests) != 1 {
		t.Fatalf("real query ran after failed dry run: %+v", runner.requests)
	}
}

type recordingBigQueryRunner struct {
	requests []BigQueryRequest
	results  []BigQueryResult
}

func (r *recordingBigQueryRunner) Run(
	_ context.Context,
	request BigQueryRequest,
) (BigQueryResult, error) {
	r.requests = append(r.requests, request)
	if len(r.results) == 0 {
		return BigQueryResult{}, nil
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result, nil
}

func parameterMap(parameters []BigQueryParameter) map[string]string {
	result := make(map[string]string, len(parameters))
	for _, parameter := range parameters {
		result[parameter.Name] = parameter.Value
	}
	return result
}
