package starhistory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	defaultBigQueryBaseURL = "https://bigquery.googleapis.com"
	bigQueryOAuthScope     = "https://www.googleapis.com/auth/bigquery"
)

// BigQueryRESTRunner 使用 jobs.query 执行参数化查询；HTTP Client 必须已经完成 OAuth。
type BigQueryRESTRunner struct {
	client  *http.Client
	baseURL *url.URL
}

// NewAuthorizedBigQueryRESTRunner 只接受 service account JSON；为空时使用受信任的 ADC。
//
// credentialsJSON 必须来自服务端 secret，不得来自 HTTP 请求或用户输入。
func NewAuthorizedBigQueryRESTRunner(
	ctx context.Context,
	credentialsJSON []byte,
) (*BigQueryRESTRunner, error) {
	var credentials *google.Credentials
	var err error
	if len(bytes.TrimSpace(credentialsJSON)) > 0 {
		credentials, err = google.CredentialsFromJSONWithType(
			ctx,
			credentialsJSON,
			google.ServiceAccount,
			bigQueryOAuthScope,
		)
	} else {
		credentials, err = google.FindDefaultCredentials(ctx, bigQueryOAuthScope)
	}
	if err != nil {
		return nil, fmt.Errorf("load BigQuery credentials: %w", err)
	}
	return NewBigQueryRESTRunner(
		oauth2.NewClient(ctx, credentials.TokenSource),
		defaultBigQueryBaseURL,
	)
}

// NewBigQueryRESTRunner 注入 HTTP Client 与 base URL，便于用 httptest 完整验证协议。
func NewBigQueryRESTRunner(client *http.Client, baseURL string) (*BigQueryRESTRunner, error) {
	if client == nil {
		return nil, fmt.Errorf("http client is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("valid BigQuery base URL is required")
	}
	return &BigQueryRESTRunner{client: client, baseURL: parsed}, nil
}

type bigQueryRESTParameter struct {
	Name           string                     `json:"name"`
	ParameterType  bigQueryRESTParameterType  `json:"parameterType"`
	ParameterValue bigQueryRESTParameterValue `json:"parameterValue"`
}

type bigQueryRESTParameterType struct {
	Type string `json:"type"`
}

type bigQueryRESTParameterValue struct {
	Value string `json:"value"`
}

type bigQueryRESTRequest struct {
	Query              string                  `json:"query"`
	UseLegacySQL       bool                    `json:"useLegacySql"`
	ParameterMode      string                  `json:"parameterMode"`
	QueryParameters    []bigQueryRESTParameter `json:"queryParameters"`
	MaximumBytesBilled string                  `json:"maximumBytesBilled"`
	DryRun             bool                    `json:"dryRun"`
	TimeoutMS          int                     `json:"timeoutMs"`
}

type bigQueryRESTResponse struct {
	JobComplete         bool   `json:"jobComplete"`
	TotalBytesProcessed string `json:"totalBytesProcessed"`
	Rows                []struct {
		Fields []struct {
			Value json.RawMessage `json:"v"`
		} `json:"f"`
	} `json:"rows"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// Run 把领域参数转换为 BigQuery REST 命名参数，并拒绝未完成或多页结果。
func (r *BigQueryRESTRunner) Run(
	ctx context.Context,
	request BigQueryRequest,
) (BigQueryResult, error) {
	if strings.TrimSpace(request.ProjectID) == "" {
		return BigQueryResult{}, fmt.Errorf("project ID is required")
	}
	if request.MaximumBytesBilled <= 0 {
		return BigQueryResult{}, fmt.Errorf("maximum bytes billed must be positive")
	}
	parameters := make([]bigQueryRESTParameter, 0, len(request.Parameters))
	for _, parameter := range request.Parameters {
		parameters = append(parameters, bigQueryRESTParameter{
			Name: parameter.Name,
			ParameterType: bigQueryRESTParameterType{
				Type: parameter.Type,
			},
			ParameterValue: bigQueryRESTParameterValue{
				Value: parameter.Value,
			},
		})
	}
	body, err := json.Marshal(bigQueryRESTRequest{
		Query:              request.SQL,
		UseLegacySQL:       false,
		ParameterMode:      "NAMED",
		QueryParameters:    parameters,
		MaximumBytesBilled: strconv.FormatInt(request.MaximumBytesBilled, 10),
		DryRun:             request.DryRun,
		TimeoutMS:          int((5 * time.Minute) / time.Millisecond),
	})
	if err != nil {
		return BigQueryResult{}, err
	}

	endpoint := r.baseURL.ResolveReference(
		&url.URL{Path: "/bigquery/v2/projects/" + url.PathEscape(request.ProjectID) + "/queries"},
	)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return BigQueryResult{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(httpRequest)
	if err != nil {
		return BigQueryResult{}, fmt.Errorf("BigQuery request: %w", err)
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return BigQueryResult{}, fmt.Errorf("read BigQuery response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return BigQueryResult{}, fmt.Errorf(
			"BigQuery HTTP %d: %s",
			response.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}

	var decoded bigQueryRESTResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return BigQueryResult{}, fmt.Errorf("decode BigQuery response: %w", err)
	}
	if decoded.Error != nil {
		return BigQueryResult{}, fmt.Errorf("BigQuery query: %s", decoded.Error.Message)
	}
	if !request.DryRun && !decoded.JobComplete {
		return BigQueryResult{}, fmt.Errorf("BigQuery query did not complete before timeout")
	}
	bytesProcessed, err := parseOptionalInt64(decoded.TotalBytesProcessed)
	if err != nil {
		return BigQueryResult{}, fmt.Errorf("parse BigQuery bytes: %w", err)
	}
	rows, err := parseBigQueryDailyRows(decoded.Rows)
	if err != nil {
		return BigQueryResult{}, err
	}
	return BigQueryResult{Rows: rows, TotalBytesProcessed: bytesProcessed}, nil
}

func parseBigQueryDailyRows(rows []struct {
	Fields []struct {
		Value json.RawMessage `json:"v"`
	} `json:"f"`
}) ([]DailyWatchEvent, error) {
	result := make([]DailyWatchEvent, 0, len(rows))
	for _, row := range rows {
		if len(row.Fields) != 2 {
			return nil, fmt.Errorf("unexpected BigQuery history column count %d", len(row.Fields))
		}
		var dayRaw, countRaw string
		if err := json.Unmarshal(row.Fields[0].Value, &dayRaw); err != nil {
			return nil, fmt.Errorf("decode BigQuery event day: %w", err)
		}
		if err := json.Unmarshal(row.Fields[1].Value, &countRaw); err != nil {
			return nil, fmt.Errorf("decode BigQuery event count: %w", err)
		}
		day, err := time.Parse("2006-01-02", dayRaw)
		if err != nil {
			return nil, fmt.Errorf("parse BigQuery event day: %w", err)
		}
		count, err := strconv.ParseInt(countRaw, 10, 64)
		if err != nil || count < 0 {
			return nil, fmt.Errorf("parse BigQuery event count %q", countRaw)
		}
		result = append(result, DailyWatchEvent{Date: day, Count: count})
	}
	return result, nil
}

func parseOptionalInt64(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

var _ BigQueryRunner = (*BigQueryRESTRunner)(nil)
