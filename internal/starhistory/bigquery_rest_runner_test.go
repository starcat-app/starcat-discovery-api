package starhistory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBigQueryRESTRunnerSendsNamedParametersAndParsesRows(t *testing.T) {
	var received bigQueryRESTRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost ||
			request.URL.Path != "/bigquery/v2/projects/starcat-project/queries" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"jobComplete": true,
			"totalBytesProcessed": "321",
			"rows": [
				{"f":[{"v":"2026-07-01"},{"v":"2"}]},
				{"f":[{"v":"2026-07-02"},{"v":"3"}]}
			]
		}`))
	}))
	defer server.Close()

	runner, err := NewBigQueryRESTRunner(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("NewBigQueryRESTRunner() error = %v", err)
	}
	result, err := runner.Run(context.Background(), BigQueryRequest{
		ProjectID: "starcat-project",
		SQL:       "SELECT @repo_id",
		Parameters: []BigQueryParameter{
			{Name: "repo_id", Type: "INT64", Value: "42"},
		},
		MaximumBytesBilled: 1024,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if received.UseLegacySQL || received.ParameterMode != "NAMED" ||
		received.MaximumBytesBilled != "1024" || len(received.QueryParameters) != 1 {
		t.Fatalf("unexpected REST request: %+v", received)
	}
	if result.TotalBytesProcessed != 321 || len(result.Rows) != 2 ||
		result.Rows[0].Count != 2 || result.Rows[1].Count != 3 {
		t.Fatalf("unexpected REST result: %+v", result)
	}
}

func TestBigQueryRESTRunnerRejectsIncompleteQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"jobComplete":false,"totalBytesProcessed":"1"}`))
	}))
	defer server.Close()
	runner, err := NewBigQueryRESTRunner(server.Client(), server.URL)
	if err != nil {
		t.Fatalf("NewBigQueryRESTRunner() error = %v", err)
	}

	_, err = runner.Run(context.Background(), BigQueryRequest{
		ProjectID:          "starcat-project",
		SQL:                "SELECT 1",
		MaximumBytesBilled: 1,
	})
	if err == nil {
		t.Fatal("incomplete BigQuery response was accepted")
	}
}
