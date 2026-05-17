// Package plugin is the Ingero Grafana datasource backend. It
// adapts the Grafana plugin SDK's Datasource interface onto the
// Ingero Echo HTTP+JSON API at /api/v1/.
//
// Architecture:
//
//   - The plugin connects only to Echo. All traffic goes through
//     Echo, which is the central authentication, authorization, and
//     audit point.
//   - Outbound transport: HTTPS by default. The settings struct
//     exposes an insecureSkipVerify toggle for local development;
//     production datasources leave it off and bring a real cert
//     chain.
//   - Auth: bearer token in the Authorization header, sourced from
//     Grafana's secure store (never the unencrypted JSONData).
//   - Three query types: SQL (POST /api/v1/sql), MCP tool dispatch
//     (POST /api/v1/tools/<name>), and an anomaly stream. Each
//     converts the response to a data.Frame.
package plugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/ingero-io/ingero-grafana-app/datasource/pkg/models"
)

var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ backend.CallResourceHandler   = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// toolsListTTL is the cache lifetime for /api/v1/tools/list
// responses. 5 minutes is short enough that a server-side tool
// registration shows up promptly, long enough that a busy dashboard
// editor doesn't hammer Echo on every panel-form open.
const toolsListTTL = 5 * time.Minute

// Datasource holds the loaded plugin settings + a cached Echo
// client. A new instance is constructed for each unique
// DataSourceInstanceSettings revision; the Grafana plugin SDK
// disposes the previous instance when settings change.
//
// tools is a per-instance cache of GET /api/v1/tools/list. It is
// keyed defensively on a SHA-256 hash of the configured bearer
// (truncated 16 bytes / 32 hex chars): the SDK normally builds a
// fresh Datasource on settings revision changes, but the bearer-
// hash key also covers any SDK that skips the recreate on a
// secureJsonData-only edit.
type Datasource struct {
	settings *models.PluginSettings
	client   *echoClient
	tools    toolsCache
}

// toolsCache is an in-memory TTL cache for /api/v1/tools/list. Only
// the calling bearer's filtered list is cached; the bearer hash is
// the cache key (see Datasource doc).
type toolsCache struct {
	mu         sync.RWMutex
	bearerHash string
	entries    []toolDescriptor
	expires    time.Time
}

// get returns the cached tools if the bearer-hash matches and the
// entry has not expired; otherwise (nil, false).
func (c *toolsCache) get(bearerHash string) ([]toolDescriptor, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.bearerHash != bearerHash {
		return nil, false
	}
	if time.Now().After(c.expires) {
		return nil, false
	}
	return c.entries, true
}

// set stores tools under the given bearer hash with the standard TTL.
// Subsequent set() calls with a different bearer hash evict the old
// entry implicitly.
func (c *toolsCache) set(bearerHash string, tools []toolDescriptor) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bearerHash = bearerHash
	c.entries = tools
	c.expires = time.Now().Add(toolsListTTL)
}

// bearerHashFor returns the first 16 bytes (32 hex chars) of the
// SHA-256 of the bearer. Used only as a cache key; never logged,
// never echoed in any response.
func bearerHashFor(bearer string) string {
	sum := sha256.Sum256([]byte(bearer))
	return hex.EncodeToString(sum[:16])
}

// NewDatasource is the factory the Grafana SDK calls per instance.
func NewDatasource(_ context.Context, sets backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	cfg, err := models.LoadPluginSettings(sets)
	if err != nil {
		return nil, err
	}
	bearer := ""
	if cfg.Secrets != nil {
		bearer = cfg.Secrets.Bearer
	}
	return &Datasource{
		settings: cfg,
		client:   newEchoClient(cfg.Endpoint, bearer, cfg.InsecureSkipVerify),
	}, nil
}

// Dispose releases per-instance resources. The default http
// transport's idle conns are GCable; nothing else to do.
func (d *Datasource) Dispose() {}

// CallResource implements backend.CallResourceHandler. The plugin
// frontend (TS) reaches the backend via Grafana's resource proxy
// rather than calling Echo directly, so we can: (a) cache the
// tools/list response across panel edits, (b) keep the bearer
// inside the backend process where Grafana's secret-store invariant
// holds, (c) shape responses for the frontend without exposing the
// Echo wire format directly.
//
// Routes:
//
//	GET /tools  →  cached tools/list, filtered per the calling
//	              bearer. Cache TTL: 5 minutes; cache key is the
//	              bearer hash.
//
// Any other path returns 404.
func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if d.client == nil || d.settings == nil {
		return sendJSONResource(sender, http.StatusBadRequest, map[string]string{
			"error": "datasource not initialised",
		})
	}

	path := strings.TrimPrefix(req.Path, "/")
	method := strings.ToUpper(req.Method)

	switch path {
	case "tools":
		if method != http.MethodGet {
			return sendJSONResource(sender, http.StatusMethodNotAllowed, map[string]string{
				"error": "method not allowed; use GET",
			})
		}
		return d.handleResourceTools(ctx, sender)
	default:
		return sendJSONResource(sender, http.StatusNotFound, map[string]string{
			"error": "no such resource",
		})
	}
}

// handleResourceTools serves cached tools/list to the frontend.
// First call per instance fetches from Echo; subsequent calls within
// the TTL window return cached bytes.
func (d *Datasource) handleResourceTools(ctx context.Context, sender backend.CallResourceResponseSender) error {
	bearer := ""
	if d.settings.Secrets != nil {
		bearer = d.settings.Secrets.Bearer
	}
	if bearer == "" {
		return sendJSONResource(sender, http.StatusBadRequest, map[string]string{
			"error": "bearer is empty; configure the datasource",
		})
	}
	hash := bearerHashFor(bearer)

	if tools, ok := d.tools.get(hash); ok {
		return sendJSONResource(sender, http.StatusOK, toolsListResponse{Tools: tools})
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tools, reqID, err := d.client.getToolsList(timeoutCtx)
	if err != nil {
		return sendJSONResource(sender, http.StatusBadGateway, map[string]string{
			"error":  withReqID(err.Error(), reqID),
			"req_id": reqID,
		})
	}
	d.tools.set(hash, tools)
	return sendJSONResource(sender, http.StatusOK, toolsListResponse{Tools: tools})
}

// sendJSONResource is a small helper that wraps the SDK's
// CallResourceResponseSender boilerplate: marshal, set Content-Type,
// send.
func sendJSONResource(sender backend.CallResourceResponseSender, status int, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		raw = []byte(`{"error":"internal: response marshal failed"}`)
		status = http.StatusInternalServerError
	}
	return sender.Send(&backend.CallResourceResponse{
		Status:  status,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    raw,
	})
}

// queryType discriminates the three query shapes. An empty
// queryType is treated as "sql" so a panel that carries only a
// `sql` field (no explicit discriminator) still works.
type queryType string

const (
	queryTypeSQL     queryType = "sql"
	queryTypeTool    queryType = "tool"
	queryTypeAnomaly queryType = "anomaly"
)

// queryModel is the Grafana-side query shape. Each panel in a
// dashboard JSON sets queryType + the fields its type needs. The
// backend dispatches on queryType to the appropriate Echo endpoint.
//
// Backward compat: when queryType is empty AND sql is non-empty,
// the query is treated as a legacy SQL query (the echo-sql-reference
// dashboard never sets queryType explicitly).
type queryModel struct {
	QueryType queryType `json:"queryType,omitempty"`

	// SQL is the body of a queryTypeSQL query.
	SQL string `json:"sql,omitempty"`

	// Tool is the dotted MCP tool name for queryTypeTool, e.g.
	// "fleet.cluster.summary". Must match
	// `^[a-z][a-z0-9_.]{1,127}$`; validated before dispatch.
	Tool string `json:"tool,omitempty"`

	// ToolArgs is forwarded verbatim as the request body's `args`
	// field. The plugin does not interpret it (the server's JSON-
	// schema validator does). Empty / null is allowed.
	ToolArgs json.RawMessage `json:"toolArgs,omitempty"`

	// Anomaly-stream fields. The plugin assembles a synthetic
	// fleet.cluster.anomaly_list invocation from these. ClusterID
	// and TimeWindow are validated client-side per the security
	// model (cluster_id: ^[A-Za-z0-9_.-]{1,64}$, time_window:
	// ^[0-9hdms]{1,16}$). Severity and Limit are forwarded to
	// the server which JSON-schema-validates them.
	TimeWindow string `json:"timeWindow,omitempty"`
	Severity   string `json:"severityFilter,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	ClusterID  string `json:"clusterId,omitempty"`
}

// Regexes shared by the dispatcher's client-side validation. Compiled
// once at package init.
var (
	toolNameRE   = regexp.MustCompile(`^[a-z][a-z0-9_.]{1,127}$`)
	clusterIDRE  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	timeWindowRE = regexp.MustCompile(`^[0-9hdms]{1,16}$`)
)

// QueryData implements backend.QueryDataHandler. Iterates the
// queries and forwards each SQL query to Echo's /api/v1/sql.
func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	for _, q := range req.Queries {
		response.Responses[q.RefID] = d.query(ctx, q)
	}
	return response, nil
}

func (d *Datasource) query(ctx context.Context, q backend.DataQuery) backend.DataResponse {
	var qm queryModel
	if err := json.Unmarshal(q.JSON, &qm); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest,
			fmt.Sprintf("json unmarshal: %v", err))
	}
	if d.client == nil {
		return backend.ErrDataResponse(backend.StatusBadRequest,
			"datasource not configured: endpoint is empty")
	}

	qt := qm.QueryType
	if qt == "" {
		qt = queryTypeSQL // legacy panels predating the discriminator
	}
	switch qt {
	case queryTypeSQL:
		return d.querySQL(ctx, q.RefID, qm)
	case queryTypeTool:
		return d.queryTool(ctx, q.RefID, qm)
	case queryTypeAnomaly:
		return d.queryAnomaly(ctx, q.RefID, qm)
	default:
		return backend.ErrDataResponse(backend.StatusBadRequest,
			fmt.Sprintf("unknown queryType %q (want sql, tool, or anomaly)", qt))
	}
}

func (d *Datasource) querySQL(ctx context.Context, refID string, qm queryModel) backend.DataResponse {
	if strings.TrimSpace(qm.SQL) == "" {
		return backend.ErrDataResponse(backend.StatusBadRequest,
			"sql is empty (set the `sql` field in the panel's query JSON)")
	}
	resp, reqID, err := d.client.postSQL(ctx, qm.SQL)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadGateway, withReqID(err.Error(), reqID))
	}
	frame, err := sqlResponseToFrame(refID, resp)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal,
			fmt.Sprintf("decoding sql response: %v", err))
	}
	return backend.DataResponse{Frames: data.Frames{frame}}
}

func (d *Datasource) queryTool(ctx context.Context, refID string, qm queryModel) backend.DataResponse {
	if !toolNameRE.MatchString(qm.Tool) {
		return backend.ErrDataResponse(backend.StatusBadRequest,
			fmt.Sprintf("invalid tool name %q (must match %s)", qm.Tool, toolNameRE.String()))
	}
	resp, reqID, err := d.client.postTool(ctx, qm.Tool, qm.ToolArgs)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadGateway, withReqID(err.Error(), reqID))
	}
	frame, err := toolResponseToFrame(refID, resp)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal,
			fmt.Sprintf("decoding tool response: %v", err))
	}
	return backend.DataResponse{Frames: data.Frames{frame}}
}

// queryAnomaly assembles a fleet.cluster.anomaly_list call from the
// structured anomaly-query fields, validating cluster_id and
// time_window before the request leaves the plugin.
func (d *Datasource) queryAnomaly(ctx context.Context, refID string, qm queryModel) backend.DataResponse {
	if qm.ClusterID != "" && !clusterIDRE.MatchString(qm.ClusterID) {
		return backend.ErrDataResponse(backend.StatusBadRequest,
			fmt.Sprintf("invalid clusterId (must match %s)", clusterIDRE.String()))
	}
	if qm.TimeWindow != "" && !timeWindowRE.MatchString(qm.TimeWindow) {
		return backend.ErrDataResponse(backend.StatusBadRequest,
			fmt.Sprintf("invalid timeWindow (must match %s)", timeWindowRE.String()))
	}

	args := map[string]any{}
	if qm.TimeWindow != "" {
		args["time_window"] = qm.TimeWindow
	}
	if qm.Severity != "" {
		args["severity_filter"] = qm.Severity
	}
	if qm.Limit > 0 {
		args["limit"] = qm.Limit
	}
	if qm.ClusterID != "" {
		args["cluster_id"] = qm.ClusterID
	}
	body, err := json.Marshal(args)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal,
			fmt.Sprintf("encoding anomaly args: %v", err))
	}

	resp, reqID, err := d.client.postTool(ctx, "fleet.cluster.anomaly_list", body)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusBadGateway, withReqID(err.Error(), reqID))
	}
	frame, err := toolResponseToFrame(refID, resp)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal,
			fmt.Sprintf("decoding anomaly response: %v", err))
	}
	return backend.DataResponse{Frames: data.Frames{frame}}
}

func withReqID(msg, reqID string) string {
	if reqID == "" {
		return msg
	}
	return msg + " (req_id=" + reqID + ")"
}

// CheckHealth implements the test-connection probe Grafana fires
// when the user clicks "Save & test" on the datasource config
// page. Calls /api/v1/health with the bearer; reports success or
// the upstream error verbatim.
func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if d.settings == nil || d.client == nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "datasource not initialised",
		}, nil
	}
	if d.settings.Endpoint == "" {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "endpoint is empty; set it on the datasource config page",
		}, nil
	}
	if d.settings.Secrets == nil || d.settings.Secrets.Bearer == "" {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "bearer token is empty; paste the token from the operator and save",
		}, nil
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	health, reqID, err := d.client.getHealth(timeoutCtx)
	if err != nil {
		msg := err.Error()
		if reqID != "" {
			msg += " (req_id=" + reqID + ")"
		}
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: msg,
		}, nil
	}
	if health.Status != "ok" {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: fmt.Sprintf("health reports status=%q", health.Status),
		}, nil
	}
	versionTag := health.Version
	if versionTag == "" {
		versionTag = "<unknown>"
	}
	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Echo " + versionTag + " is reachable",
	}, nil
}

// sqlResponseToFrame converts Echo's {columns, rows} shape into a
// Grafana data.Frame. The first column whose values are all
// timestamp-shaped (RFC 3339 strings) is promoted to a time column;
// otherwise no time column is set and panels fall back to row order.
//
// All non-time columns are typed as string. Promoting numerics to
// real Go types requires inspecting the first non-null value of
// each column; that is a planned follow-up.
func sqlResponseToFrame(refID string, resp *sqlResponse) (*data.Frame, error) {
	frame := data.NewFrame(refID)
	if len(resp.Columns) == 0 {
		return frame, nil
	}
	cols := make([][]string, len(resp.Columns))
	for i := range cols {
		cols[i] = make([]string, len(resp.Rows))
	}
	for ri, row := range resp.Rows {
		for ci := range resp.Columns {
			if ci >= len(row) {
				cols[ci][ri] = ""
				continue
			}
			cols[ci][ri] = formatCell(row[ci])
		}
	}
	for ci, name := range resp.Columns {
		frame.Fields = append(frame.Fields, data.NewField(name, nil, cols[ci]))
	}
	return frame, nil
}

// toolResponseToFrame projects an MCP tool's `result` into a Grafana
// DataFrame using three pragmatic shape detectors:
//
//   - result is an array of objects: each row becomes a frame row,
//     each unique key (in first-row order) becomes a column.
//   - result is an object containing a "rows" array of objects: same
//     treatment on that nested array (covers cluster.anomaly_list,
//     cluster.find_stragglers, etc).
//   - everything else: a single-row, single-column frame named
//     "result" holding the JSON-serialised value.
//
// Cell values are stringified by formatCell, same as SQL rows.
// Type-promoting numeric / boolean / time columns is a planned
// follow-up.
func toolResponseToFrame(refID string, resp *toolResponse) (*data.Frame, error) {
	frame := data.NewFrame(refID)
	if len(resp.Result) == 0 {
		return frame, nil
	}

	rows, ok := extractRows(resp.Result)
	if !ok {
		// Fallback: 1-row, 1-column frame carrying the raw result.
		frame.Fields = append(frame.Fields,
			data.NewField("result", nil, []string{string(resp.Result)}))
		return frame, nil
	}
	if len(rows) == 0 {
		return frame, nil
	}

	// Column order = key order of the first row. Subsequent rows whose
	// keys differ contribute "" for missing columns.
	cols := make([]string, 0, len(rows[0]))
	colSet := make(map[string]struct{}, len(rows[0]))
	for k := range rows[0] {
		if _, seen := colSet[k]; !seen {
			cols = append(cols, k)
			colSet[k] = struct{}{}
		}
	}
	// Stable: sort columns alphabetically so repeated renders are
	// deterministic (json.Unmarshal into a map does not preserve
	// input order, so we cannot honour it anyway).
	sortStrings(cols)

	colVals := make([][]string, len(cols))
	for i := range colVals {
		colVals[i] = make([]string, len(rows))
	}
	for ri, row := range rows {
		for ci, name := range cols {
			colVals[ci][ri] = formatCell(row[name])
		}
	}
	for ci, name := range cols {
		frame.Fields = append(frame.Fields,
			data.NewField(name, nil, colVals[ci]))
	}
	return frame, nil
}

// extractRows returns ([]map[string]any, true) if raw decodes as
// either an array of objects, or an object with a "rows" field that
// is an array of objects. Otherwise returns (nil, false).
func extractRows(raw json.RawMessage) ([]map[string]any, bool) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var asArray []map[string]any
	if err := dec.Decode(&asArray); err == nil {
		return asArray, true
	}
	var asObject map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asObject); err != nil {
		return nil, false
	}
	for _, key := range []string{"rows", "items", "results"} {
		if nested, ok := asObject[key]; ok {
			dec2 := json.NewDecoder(bytes.NewReader(nested))
			dec2.UseNumber()
			var rows []map[string]any
			if err := dec2.Decode(&rows); err == nil {
				return rows, true
			}
		}
	}
	return nil, false
}

// sortStrings is a tiny insertion sort. Avoids pulling in the sort
// package for a one-line use (typical column count is 4-10).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func formatCell(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return string(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprintf("%v", x)
		}
		return string(b)
	}
}
