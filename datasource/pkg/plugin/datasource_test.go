package plugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/ingero-io/ingero-grafana-app/datasource/pkg/models"
)

// newDatasourceFor builds a Datasource pointed at the given test
// server URL, with a fixed bearer + insecureSkipVerify=true so the
// httptest TLS cert is accepted.
func newDatasourceFor(t *testing.T, baseURL, bearer string) *Datasource {
	t.Helper()
	return &Datasource{
		settings: &models.PluginSettings{
			Endpoint:           baseURL,
			InsecureSkipVerify: true,
			Secrets:            &models.SecretPluginSettings{Bearer: bearer},
		},
		client: newEchoClient(baseURL, bearer, true),
	}
}

func TestCheckHealth_EmptyEndpoint(t *testing.T) {
	d := &Datasource{
		settings: &models.PluginSettings{
			Endpoint: "",
			Secrets:  &models.SecretPluginSettings{Bearer: "x"},
		},
		client: newEchoClient("", "x", false),
	}
	res, err := d.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != backend.HealthStatusError {
		t.Errorf("status = %v, want Error", res.Status)
	}
	if !strings.Contains(res.Message, "endpoint is empty") {
		t.Errorf("message = %q; want endpoint-empty hint", res.Message)
	}
}

func TestCheckHealth_EmptyBearer(t *testing.T) {
	d := &Datasource{
		settings: &models.PluginSettings{
			Endpoint: "http://x",
			Secrets:  &models.SecretPluginSettings{Bearer: ""},
		},
		client: newEchoClient("http://x", "", false),
	}
	res, err := d.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != backend.HealthStatusError {
		t.Errorf("status = %v, want Error", res.Status)
	}
	if !strings.Contains(res.Message, "bearer") {
		t.Errorf("message = %q; want bearer-empty hint", res.Message)
	}
}

func TestCheckHealth_Success(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/health" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret-token" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("X-Request-Id", "deadbeefcafe1234")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":  "ok",
			"version": "v1.2.3",
		})
	}))
	defer srv.Close()

	d := newDatasourceFor(t, srv.URL, "secret-token")
	res, err := d.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != backend.HealthStatusOk {
		t.Errorf("status = %v, want Ok; message=%q", res.Status, res.Message)
	}
	if !strings.Contains(res.Message, "v1.2.3") {
		t.Errorf("message = %q; want version surfaced", res.Message)
	}
}

func TestCheckHealth_401(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "abcd0000abcd0000")
		http.Error(w, "invalid bearer token", http.StatusUnauthorized)
	}))
	defer srv.Close()

	d := newDatasourceFor(t, srv.URL, "wrong")
	res, err := d.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != backend.HealthStatusError {
		t.Errorf("status = %v, want Error", res.Status)
	}
	if !strings.Contains(res.Message, "401") {
		t.Errorf("message = %q; want status 401 surfaced", res.Message)
	}
	if !strings.Contains(res.Message, "req_id=abcd0000abcd0000") {
		t.Errorf("message = %q; want req_id surfaced for support correlation", res.Message)
	}
}

func TestQueryData_SQL_Success(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/sql" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		var body sqlRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("body decode: %v", err)
		}
		if !strings.Contains(body.SQL, "SELECT") {
			t.Errorf("forwarded sql = %q", body.SQL)
		}
		w.Header().Set("X-Request-Id", "abc123")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"columns": []string{"node", "cnt"},
			"rows": [][]any{
				{"gpu-01", 42},
				{"gpu-02", 17},
			},
		})
	}))
	defer srv.Close()

	d := newDatasourceFor(t, srv.URL, "ok")
	req := &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{
				RefID: "A",
				JSON:  []byte(`{"sql":"SELECT node, COUNT(*) AS cnt FROM events GROUP BY node"}`),
			},
		},
	}
	resp, err := d.QueryData(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := resp.Responses["A"]
	if r.Error != nil {
		t.Fatalf("response error: %v", r.Error)
	}
	if len(r.Frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(r.Frames))
	}
	f := r.Frames[0]
	if len(f.Fields) != 2 {
		t.Fatalf("fields = %d, want 2", len(f.Fields))
	}
	if f.Fields[0].Name != "node" || f.Fields[1].Name != "cnt" {
		t.Errorf("field names = %q,%q", f.Fields[0].Name, f.Fields[1].Name)
	}
	if got := f.Fields[0].At(0).(string); got != "gpu-01" {
		t.Errorf("row[0] node = %q", got)
	}
	// JSON numbers come through as json.Number/float; formatCell
	// renders them as their canonical string form.
	if got := f.Fields[1].At(0).(string); got != "42" {
		t.Errorf("row[0] cnt = %q, want 42", got)
	}
}

func TestQueryData_SQL_EmptyQuery(t *testing.T) {
	d := newDatasourceFor(t, "http://x", "x")
	req := &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{RefID: "A", JSON: []byte(`{"sql":""}`)},
		},
	}
	resp, err := d.QueryData(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := resp.Responses["A"]
	if r.Error == nil {
		t.Fatalf("expected an error response for empty SQL")
	}
	if !strings.Contains(r.Error.Error(), "sql is empty") {
		t.Errorf("error = %q; want sql-empty hint", r.Error)
	}
}

func TestQueryData_SQL_UpstreamError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "fail0001")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "sql is not read-only",
			"code":  "sql_not_read_only",
		})
	}))
	defer srv.Close()

	d := newDatasourceFor(t, srv.URL, "ok")
	req := &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{RefID: "A", JSON: []byte(`{"sql":"DROP TABLE events"}`)},
		},
	}
	resp, err := d.QueryData(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := resp.Responses["A"]
	if r.Error == nil {
		t.Fatalf("expected an error response for SQL write-verb")
	}
	if !strings.Contains(r.Error.Error(), "sql_not_read_only") {
		t.Errorf("error = %q; want code surfaced for client-side classification", r.Error)
	}
	if !strings.Contains(r.Error.Error(), "req_id=fail0001") {
		t.Errorf("error = %q; want req_id surfaced", r.Error)
	}
}

func TestSqlResponseToFrame_EmptyColumns(t *testing.T) {
	resp := &sqlResponse{Columns: nil, Rows: nil}
	f, err := sqlResponseToFrame("X", resp)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(f.Fields) != 0 {
		t.Errorf("fields = %d, want 0", len(f.Fields))
	}
}

func TestSqlResponseToFrame_ShortRow(t *testing.T) {
	// A row shorter than the column count should pad with empty
	// strings instead of panicking.
	resp := &sqlResponse{
		Columns: []string{"a", "b", "c"},
		Rows: [][]any{
			{"x", "y"}, // missing column c
		},
	}
	f, err := sqlResponseToFrame("X", resp)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(f.Fields) != 3 {
		t.Fatalf("fields = %d, want 3", len(f.Fields))
	}
	if got := f.Fields[2].At(0).(string); got != "" {
		t.Errorf("padded cell = %q, want empty", got)
	}
}

func TestFormatCell(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"hello", "hello"},
		{json.Number("3.14"), "3.14"},
		{true, "true"},
		{false, "false"},
		{[]int{1, 2}, "[1,2]"},
		{map[string]int{"a": 1}, `{"a":1}`},
	}
	for _, c := range cases {
		got := formatCell(c.in)
		if got != c.want {
			t.Errorf("formatCell(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// queryOne is a test helper: run a single query through the
// datasource and return the resulting DataResponse.
func queryOne(t *testing.T, d *Datasource, qm queryModel) backend.DataResponse {
	t.Helper()
	raw, err := json.Marshal(qm)
	if err != nil {
		t.Fatalf("marshal queryModel: %v", err)
	}
	resp, err := d.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: raw}},
	})
	if err != nil {
		t.Fatalf("QueryData: %v", err)
	}
	return resp.Responses["A"]
}

func TestQuery_BackwardCompatSQL(t *testing.T) {
	// A pre-discriminator query (empty queryType + sql set) must
	// still hit the SQL path. echo-sql-reference's panels predate
	// the discriminator.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/sql" {
			t.Errorf("path = %q, want /api/v2/sql", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"columns":["n"],"rows":[[1]]}`))
	}))
	defer srv.Close()

	d := newDatasourceFor(t, srv.URL, "tok")
	got := queryOne(t, d, queryModel{SQL: "SELECT 1"})
	if got.Error != nil {
		t.Fatalf("error: %v", got.Error)
	}
	if len(got.Frames) != 1 || len(got.Frames[0].Fields) != 1 {
		t.Fatalf("frames = %v", got.Frames)
	}
}

func TestQuery_ToolHappyPath(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/tools/fleet.cluster.summary" {
			t.Errorf("path = %q, want /api/v2/tools/fleet.cluster.summary", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing bearer header: %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"rows":[{"host":"n1","ok":true},{"host":"n2","ok":false}]}}`))
	}))
	defer srv.Close()

	d := newDatasourceFor(t, srv.URL, "tok")
	got := queryOne(t, d, queryModel{
		QueryType: queryTypeTool,
		Tool:      "fleet.cluster.summary",
	})
	if got.Error != nil {
		t.Fatalf("error: %v", got.Error)
	}
	if len(got.Frames) != 1 {
		t.Fatalf("frames = %d", len(got.Frames))
	}
	f := got.Frames[0]
	if len(f.Fields) != 2 {
		t.Fatalf("fields = %d, want 2 (host, ok)", len(f.Fields))
	}
	// Columns are sorted alphabetically for determinism.
	if f.Fields[0].Name != "host" || f.Fields[1].Name != "ok" {
		t.Errorf("columns = %s, %s; want host, ok", f.Fields[0].Name, f.Fields[1].Name)
	}
	if f.Fields[0].Len() != 2 {
		t.Errorf("rows = %d, want 2", f.Fields[0].Len())
	}
}

func TestQuery_ToolErrorPath(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "abc123")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"tenant scope mismatch","code":"acl_denied"}`))
	}))
	defer srv.Close()

	d := newDatasourceFor(t, srv.URL, "tok")
	got := queryOne(t, d, queryModel{
		QueryType: queryTypeTool,
		Tool:      "fleet.cluster.run_analysis",
	})
	if got.Error == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(got.Error.Error(), "tenant scope mismatch") {
		t.Errorf("error = %q; want tenant-scope hint", got.Error.Error())
	}
	if !strings.Contains(got.Error.Error(), "req_id=abc123") {
		t.Errorf("error = %q; want req_id surfaced", got.Error.Error())
	}
}

func TestQuery_ToolNameValidation(t *testing.T) {
	// No HTTP fixture: dispatch should reject before the request.
	d := newDatasourceFor(t, "https://unreachable.invalid", "tok")
	cases := []struct {
		name string
		tool string
	}{
		{"empty", ""},
		{"uppercase", "Fleet.cluster.summary"},
		{"contains-slash", "fleet/cluster/summary"},
		{"contains-space", "fleet.cluster summary"},
		{"sql-injection-ish", "fleet.cluster; DROP TABLE events"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := queryOne(t, d, queryModel{QueryType: queryTypeTool, Tool: c.tool})
			if got.Error == nil {
				t.Fatalf("expected validation error, got nil")
			}
			if !strings.Contains(got.Error.Error(), "invalid tool name") {
				t.Errorf("error = %q; want tool-name validation hint", got.Error.Error())
			}
		})
	}
}

func TestQuery_AnomalyHappyPath(t *testing.T) {
	var capturedBody []byte
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/tools/fleet.cluster.anomaly_list" {
			t.Errorf("path = %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		capturedBody = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"rows":[{"ts":"2026-05-14T10:00:00Z","severity":"warn","summary":"x"}]}}`))
	}))
	defer srv.Close()

	d := newDatasourceFor(t, srv.URL, "tok")
	got := queryOne(t, d, queryModel{
		QueryType:  queryTypeAnomaly,
		TimeWindow: "1h",
		Severity:   "warn",
		Limit:      100,
		ClusterID:  "prod-a",
	})
	if got.Error != nil {
		t.Fatalf("error: %v", got.Error)
	}
	// Body should contain time_window, severity_filter, limit, cluster_id.
	var sent struct {
		Args map[string]any `json:"args"`
	}
	if err := json.Unmarshal(capturedBody, &sent); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	if sent.Args["time_window"] != "1h" {
		t.Errorf("time_window = %v, want 1h", sent.Args["time_window"])
	}
	if sent.Args["severity_filter"] != "warn" {
		t.Errorf("severity_filter = %v, want warn", sent.Args["severity_filter"])
	}
	if sent.Args["cluster_id"] != "prod-a" {
		t.Errorf("cluster_id = %v, want prod-a", sent.Args["cluster_id"])
	}
}

func TestQuery_AnomalyValidation(t *testing.T) {
	d := newDatasourceFor(t, "https://unreachable.invalid", "tok")
	cases := []struct {
		name  string
		qm    queryModel
		field string // substring expected in the error message
	}{
		{
			name:  "cluster_id with CRLF",
			qm:    queryModel{QueryType: queryTypeAnomaly, ClusterID: "prod\r\nINJECTED"},
			field: "invalid clusterId",
		},
		{
			name:  "cluster_id too long",
			qm:    queryModel{QueryType: queryTypeAnomaly, ClusterID: strings.Repeat("a", 65)},
			field: "invalid clusterId",
		},
		{
			name:  "cluster_id with semicolon",
			qm:    queryModel{QueryType: queryTypeAnomaly, ClusterID: "prod-a; DROP"},
			field: "invalid clusterId",
		},
		{
			name:  "time_window with alpha-numeric mix outside grammar",
			qm:    queryModel{QueryType: queryTypeAnomaly, TimeWindow: "1week"},
			field: "invalid timeWindow",
		},
		{
			name:  "time_window with negative",
			qm:    queryModel{QueryType: queryTypeAnomaly, TimeWindow: "-1h"},
			field: "invalid timeWindow",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := queryOne(t, d, c.qm)
			if got.Error == nil {
				t.Fatalf("expected validation error")
			}
			if !strings.Contains(got.Error.Error(), c.field) {
				t.Errorf("error = %q; want %q hint", got.Error.Error(), c.field)
			}
		})
	}
}

func TestQuery_UnknownQueryType(t *testing.T) {
	d := newDatasourceFor(t, "https://unreachable.invalid", "tok")
	got := queryOne(t, d, queryModel{QueryType: "synthwave"})
	if got.Error == nil || !strings.Contains(got.Error.Error(), "unknown queryType") {
		t.Errorf("expected unknown-queryType error, got %v", got.Error)
	}
}

func TestToolResponseToFrame_ScalarFallback(t *testing.T) {
	resp := &toolResponse{Result: json.RawMessage(`42`)}
	f, err := toolResponseToFrame("X", resp)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(f.Fields) != 1 || f.Fields[0].Name != "result" {
		t.Fatalf("fields = %v", f.Fields)
	}
	if got := f.Fields[0].At(0).(string); got != "42" {
		t.Errorf("cell = %q, want 42", got)
	}
}

func TestToolResponseToFrame_DirectArray(t *testing.T) {
	resp := &toolResponse{Result: json.RawMessage(`[{"a":1,"b":"x"},{"a":2,"b":"y"}]`)}
	f, err := toolResponseToFrame("X", resp)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(f.Fields) != 2 {
		t.Fatalf("fields = %d, want 2", len(f.Fields))
	}
	if f.Fields[0].Len() != 2 {
		t.Errorf("rows = %d, want 2", f.Fields[0].Len())
	}
}

func TestToolResponseToFrame_ItemsKey(t *testing.T) {
	resp := &toolResponse{Result: json.RawMessage(`{"items":[{"k":"v"}]}`)}
	f, err := toolResponseToFrame("X", resp)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(f.Fields) != 1 || f.Fields[0].Name != "k" {
		t.Fatalf("fields = %v", f.Fields)
	}
}

// captureSender is a backend.CallResourceResponseSender that records
// the last response it received. Used by the CallResource tests.
type captureSender struct {
	resp *backend.CallResourceResponse
}

func (c *captureSender) Send(r *backend.CallResourceResponse) error {
	c.resp = r
	return nil
}

func TestCallResource_ToolsHappyPath(t *testing.T) {
	calls := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/tools/list" {
			t.Errorf("path = %q, want /api/v2/tools/list", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing bearer; got %q", r.Header.Get("Authorization"))
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tools":[{"name":"fleet.cluster.summary","description":"d","input_schema":{"type":"object"}}]}`))
	}))
	defer srv.Close()

	d := newDatasourceFor(t, srv.URL, "tok")
	sender := &captureSender{}
	if err := d.CallResource(context.Background(),
		&backend.CallResourceRequest{Method: "GET", Path: "tools"}, sender); err != nil {
		t.Fatalf("CallResource: %v", err)
	}
	if sender.resp == nil || sender.resp.Status != 200 {
		t.Fatalf("status = %v", sender.resp)
	}
	var body toolsListResponse
	if err := json.Unmarshal(sender.resp.Body, &body); err != nil {
		t.Fatalf("body unmarshal: %v", err)
	}
	if len(body.Tools) != 1 || body.Tools[0].Name != "fleet.cluster.summary" {
		t.Errorf("tools = %+v", body.Tools)
	}

	// Second call should hit the cache; upstream calls should stay at 1.
	sender2 := &captureSender{}
	if err := d.CallResource(context.Background(),
		&backend.CallResourceRequest{Method: "GET", Path: "tools"}, sender2); err != nil {
		t.Fatalf("CallResource second: %v", err)
	}
	if calls != 1 {
		t.Errorf("upstream calls = %d, want 1 (cache miss)", calls)
	}
}

func TestCallResource_UnknownPath(t *testing.T) {
	d := newDatasourceFor(t, "https://unreachable.invalid", "tok")
	sender := &captureSender{}
	if err := d.CallResource(context.Background(),
		&backend.CallResourceRequest{Method: "GET", Path: "whoami"}, sender); err != nil {
		t.Fatalf("CallResource: %v", err)
	}
	if sender.resp.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", sender.resp.Status)
	}
}

func TestCallResource_MethodNotAllowed(t *testing.T) {
	d := newDatasourceFor(t, "https://unreachable.invalid", "tok")
	sender := &captureSender{}
	if err := d.CallResource(context.Background(),
		&backend.CallResourceRequest{Method: "POST", Path: "tools"}, sender); err != nil {
		t.Fatalf("CallResource: %v", err)
	}
	if sender.resp.Status != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", sender.resp.Status)
	}
}

func TestCallResource_EmptyBearer(t *testing.T) {
	d := &Datasource{
		settings: &models.PluginSettings{
			Endpoint: "https://unreachable.invalid",
			Secrets:  &models.SecretPluginSettings{Bearer: ""},
		},
		client: newEchoClient("https://unreachable.invalid", "", true),
	}
	sender := &captureSender{}
	if err := d.CallResource(context.Background(),
		&backend.CallResourceRequest{Method: "GET", Path: "tools"}, sender); err != nil {
		t.Fatalf("CallResource: %v", err)
	}
	if sender.resp.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", sender.resp.Status)
	}
}

func TestToolsCache_BearerHashKey(t *testing.T) {
	// Different bearers must produce different cache entries even
	// when (hypothetically) the same Datasource instance is reused.
	c := toolsCache{}
	toolsA := []toolDescriptor{{Name: "a"}}
	toolsB := []toolDescriptor{{Name: "b"}}
	c.set(bearerHashFor("alpha"), toolsA)
	if got, ok := c.get(bearerHashFor("alpha")); !ok || len(got) != 1 || got[0].Name != "a" {
		t.Fatalf("alpha get: %+v ok=%v", got, ok)
	}
	if got, ok := c.get(bearerHashFor("beta")); ok {
		t.Errorf("beta should miss; got %+v", got)
	}
	c.set(bearerHashFor("beta"), toolsB)
	if got, ok := c.get(bearerHashFor("beta")); !ok || got[0].Name != "b" {
		t.Fatalf("beta get: %+v ok=%v", got, ok)
	}
	// Re-set on beta overwrites; old alpha entry is gone because
	// the cache stores one entry total (not a map). Alpha-keyed
	// get should now miss.
	if _, ok := c.get(bearerHashFor("alpha")); ok {
		t.Errorf("alpha should miss after beta set")
	}
}

func TestToolsCache_TTLExpiry(t *testing.T) {
	c := toolsCache{}
	c.set(bearerHashFor("x"), []toolDescriptor{{Name: "n"}})
	if _, ok := c.get(bearerHashFor("x")); !ok {
		t.Fatalf("should hit immediately")
	}
	// Force expiry by setting expires to a past time.
	c.mu.Lock()
	c.expires = time.Now().Add(-time.Second)
	c.mu.Unlock()
	if _, ok := c.get(bearerHashFor("x")); ok {
		t.Errorf("should miss after expiry")
	}
}

func TestBearerHashFor_StableAndDistinct(t *testing.T) {
	if bearerHashFor("a") != bearerHashFor("a") {
		t.Errorf("not stable across calls")
	}
	if bearerHashFor("a") == bearerHashFor("b") {
		t.Errorf("collision on a vs b")
	}
	// Hash is 32 hex chars (16 bytes).
	if len(bearerHashFor("x")) != 32 {
		t.Errorf("hash len = %d, want 32 hex", len(bearerHashFor("x")))
	}
}
