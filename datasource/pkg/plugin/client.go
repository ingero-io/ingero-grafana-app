package plugin

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// echoClient is a minimal HTTP client for the Ingero Echo HTTP+JSON
// API at /api/v1/...
//
// Built to be cheap: one shared transport with a 30s timeout, a
// connection pool sized for the typical Grafana panel refresh
// pattern (panels fan out 1-5 queries per refresh; 16 idle conns
// per host is comfortable headroom), and a single Authorization
// header attached on every request.
//
// TLS posture: by default the http.Transport's tls.Config is the Go
// default (system roots, certificate verification on). When the
// datasource settings carry insecureSkipVerify=true, the transport
// uses InsecureSkipVerify=true. Documented in the settings struct.
type echoClient struct {
	base   string // e.g. "https://echo.internal:8081"
	bearer string
	hc     *http.Client
}

func newEchoClient(base, bearer string, insecureSkipVerify bool) *echoClient {
	tr := &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     90 * time.Second,
	}
	if insecureSkipVerify {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &echoClient{
		base:   strings.TrimRight(base, "/"),
		bearer: bearer,
		hc: &http.Client{
			Transport: tr,
			Timeout:   30 * time.Second,
		},
	}
}

// versionsResponse mirrors the Echo /api/versions shape. Only the
// fields the plugin actually needs are decoded; unknown fields are
// ignored so a server-side addition doesn't break the plugin.
type versionsResponse struct {
	Supported []string `json:"supported"`
	Preferred string   `json:"preferred"`
	Binary    struct {
		Component string `json:"component"`
		Version   string `json:"version"`
	} `json:"binary"`
	Capabilities map[string]bool `json:"capabilities"`
}

// healthResponse mirrors the Echo /api/v1/health shape.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version,omitempty"`
}

// sqlRequest is the body shape POSTed to /api/v1/sql.
type sqlRequest struct {
	SQL string `json:"sql"`
}

// sqlResponse mirrors Echo's SQL response. columns is the ordered
// list of column names; rows is a 2D array where rows[i][j] is the
// value at column j of row i (json.Number / string / bool / nil).
type sqlResponse struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

// toolDescriptor mirrors Echo's tools/list entry shape. input_schema
// is surfaced by the server; output_schema may be empty depending on
// the Echo version. Schemas are kept as raw bytes so the plugin
// hands them through to the frontend unmodified.
type toolDescriptor struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
}

// toolsListResponse mirrors Echo's GET /api/v1/tools/list body.
// Tenant-scoped bearers see a filtered subset; the plugin caches the
// list as returned to the calling bearer (no cross-bearer pollution).
type toolsListResponse struct {
	Tools []toolDescriptor `json:"tools"`
}

// toolResponse mirrors POST /api/v1/tools/<name>. The Result field
// carries the tool's declared output shape; the plugin handles three
// common cases when projecting it to a Grafana DataFrame:
//   - result is an array of objects: each key becomes a column
//   - result has a "rows" field that is an array of objects: same
//   - everything else: single-row, single-column frame with the
//     result serialised back to JSON
//
// More precise per-tool shaping is a planned follow-up: a
// schema-driven query editor that reads each tool's declared
// output_schema and binds columns by schema rather than by JSON
// shape detection.
type toolResponse struct {
	Result json.RawMessage `json:"result"`
}

// echoError is the shape Echo returns on 4xx / 5xx. The plugin
// surfaces .Error verbatim in CheckHealth + QueryData responses;
// .Code lets future logic branch on machine-stable identifiers
// (rate_limited, in_flight_cap, sql_not_read_only, etc).
type echoError struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// getVersions issues GET /api/versions (no bearer required).
func (c *echoClient) getVersions(ctx context.Context) (*versionsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/versions", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("versions: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("versions: status %d", resp.StatusCode)
	}
	var v versionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, fmt.Errorf("versions: decode: %w", err)
	}
	return &v, nil
}

// getHealth issues GET /api/v1/health with the configured bearer.
// Returns the parsed body and the X-Request-Id header value for
// diagnostic surface in CheckHealth messages.
func (c *echoClient) getHealth(ctx context.Context) (*healthResponse, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/health", nil)
	if err != nil {
		return nil, "", err
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("health: %w", err)
	}
	defer resp.Body.Close()
	reqID := resp.Header.Get("X-Request-Id")
	if resp.StatusCode != http.StatusOK {
		return nil, reqID, fmt.Errorf("health: status %d", resp.StatusCode)
	}
	var h healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return nil, reqID, fmt.Errorf("health: decode: %w", err)
	}
	return &h, reqID, nil
}

// postSQL issues POST /api/v1/sql with the configured bearer. On
// non-200 responses, tries to decode an echoError and surfaces it
// via the returned error; on transport errors, returns the wrapped
// error.
func (c *echoClient) postSQL(ctx context.Context, sql string) (*sqlResponse, string, error) {
	body, err := json.Marshal(sqlRequest{SQL: sql})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/api/v1/sql", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("sql: %w", err)
	}
	defer resp.Body.Close()
	reqID := resp.Header.Get("X-Request-Id")

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		var e echoError
		if jerr := json.Unmarshal(raw, &e); jerr == nil && e.Error != "" {
			return nil, reqID, fmt.Errorf("sql: %s (status %d, code %q)",
				e.Error, resp.StatusCode, e.Code)
		}
		return nil, reqID, fmt.Errorf("sql: status %d: %s",
			resp.StatusCode, truncate(string(raw), 256))
	}

	var s sqlResponse
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, reqID, fmt.Errorf("sql: decode: %w", err)
	}
	return &s, reqID, nil
}

// getToolsList issues GET /api/v1/tools/list with the configured
// bearer. The server filters the response per the calling bearer,
// so the plugin caches the result under that bearer's hash: a
// tenant-scoped instance's cache never carries tools that a
// wider-scoped bearer would see, and vice versa.
func (c *echoClient) getToolsList(ctx context.Context) ([]toolDescriptor, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base+"/api/v1/tools/list", nil)
	if err != nil {
		return nil, "", err
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("tools/list: %w", err)
	}
	defer resp.Body.Close()
	reqID := resp.Header.Get("X-Request-Id")
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		var e echoError
		if jerr := json.Unmarshal(raw, &e); jerr == nil && e.Error != "" {
			return nil, reqID, fmt.Errorf("tools/list: %s (status %d, code %q)",
				e.Error, resp.StatusCode, e.Code)
		}
		return nil, reqID, fmt.Errorf("tools/list: status %d: %s",
			resp.StatusCode, truncate(string(raw), 256))
	}
	var body toolsListResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, reqID, fmt.Errorf("tools/list: decode: %w", err)
	}
	return body.Tools, reqID, nil
}

// postTool issues POST /api/v1/tools/<name> with the configured
// bearer. Tool name is path-segment-escaped: callers pass the dotted
// name (fleet.cluster.summary) as-is. The args body is forwarded
// verbatim as the request body's `args` field. On non-200 responses
// the same echoError decode path as postSQL applies.
//
// Tool name validation: callers MUST ensure name matches the
// dispatch route pattern `^[a-z][a-z0-9_.]{1,127}$`. The plugin
// validates this at the query() layer before calling postTool, so
// this method trusts its input.
func (c *echoClient) postTool(ctx context.Context, name string, args json.RawMessage) (*toolResponse, string, error) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	envelope, err := json.Marshal(struct {
		Args json.RawMessage `json:"args"`
	}{Args: args})
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.base+"/api/v1/tools/"+name, bytes.NewReader(envelope))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("tool %s: %w", name, err)
	}
	defer resp.Body.Close()
	reqID := resp.Header.Get("X-Request-Id")

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		var e echoError
		if jerr := json.Unmarshal(raw, &e); jerr == nil && e.Error != "" {
			return nil, reqID, fmt.Errorf("tool %s: %s (status %d, code %q)",
				name, e.Error, resp.StatusCode, e.Code)
		}
		return nil, reqID, fmt.Errorf("tool %s: status %d: %s",
			name, resp.StatusCode, truncate(string(raw), 256))
	}

	var t toolResponse
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, reqID, fmt.Errorf("tool %s: decode: %w", name, err)
	}
	return &t, reqID, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
