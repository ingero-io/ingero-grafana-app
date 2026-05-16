// fake-echo is a tiny scripted HTTP server that mimics the Ingero
// Echo HTTP API surface used by this plugin's e2e tests.
//
// It is intentionally NOT a re-implementation of Echo: it returns
// scripted responses keyed by request shape, validates the bearer
// shape so plugin bearer-handling code paths are exercised, and
// writes every received request to a request log file so Playwright
// tests can assert "Echo-B was the one that got hit after the
// $ingero_source switch".
//
// CLI:
//
//	fake-echo --addr 127.0.0.1:8081 --bearer dev-bearer \
//	          --label echo-a --request-log /tmp/echo-a.jsonl
//
// Two instances side by side cover the multi-instance e2e case.
// Each response carries an `_echo_label` field that tests assert on.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type config struct {
	addr           string
	bearer         string
	label          string
	requestLogPath string
}

func parseFlags() config {
	c := config{}
	flag.StringVar(&c.addr, "addr", "127.0.0.1:8081", "listen address")
	flag.StringVar(&c.bearer, "bearer", "dev-bearer", "expected bearer token")
	flag.StringVar(&c.label, "label", "echo", "label identifying this fake (appears in response bodies)")
	flag.StringVar(&c.requestLogPath, "request-log", "", "if set, append every request as a JSONL line to this file")
	flag.Parse()
	return c
}

// server holds the wired routes + scripted-response state.
type server struct {
	cfg       config
	logFile   *os.File
	logMu     sync.Mutex
	startedAt time.Time
}

// loggedRequest is the JSONL record written for every request. Tests
// load this file to assert which fake-Echo got hit.
type loggedRequest struct {
	Time   time.Time `json:"time"`
	Method string    `json:"method"`
	Path   string    `json:"path"`
	Bearer bool      `json:"bearer_present"`
	Label  string    `json:"label"`
	Status int       `json:"status"`
}

func (s *server) record(r *http.Request, status int) {
	if s.logFile == nil {
		return
	}
	rec := loggedRequest{
		Time:   time.Now().UTC(),
		Method: r.Method,
		Path:   r.URL.Path,
		Bearer: strings.HasPrefix(r.Header.Get("Authorization"), "Bearer "),
		Label:  s.cfg.label,
		Status: status,
	}
	b, _ := json.Marshal(rec)
	s.logMu.Lock()
	defer s.logMu.Unlock()
	_, _ = s.logFile.Write(append(b, '\n'))
}

// requireBearer checks the Authorization header against the expected
// bearer. Returns true if the request should proceed.
func (s *server) requireBearer(w http.ResponseWriter, r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if auth != "Bearer "+s.cfg.bearer {
		s.write(w, r, http.StatusUnauthorized, map[string]any{
			"error":        "unauthorized",
			"code":         "missing_bearer",
			"_echo_label":  s.cfg.label,
		})
		return false
	}
	return true
}

// write is the shared JSON-response helper. It also stamps every
// response with X-Request-Id (the plugin uses it for error correlation)
// and records the request to the log.
func (s *server) write(w http.ResponseWriter, r *http.Request, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Request-Id", fmt.Sprintf("fake-%s-%d", s.cfg.label, time.Now().UnixNano()))
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
	s.record(r, status)
}

// /api/versions (unauthenticated; major.minor version only).
func (s *server) handleVersions(w http.ResponseWriter, r *http.Request) {
	s.write(w, r, http.StatusOK, map[string]any{
		"supported":  []string{"v1"},
		"deprecated": []string{},
		"preferred":  "v1",
		"binary": map[string]string{
			"component": "echo",
			"version":   "v1.0",
		},
		"capabilities": map[string]bool{
			"tools_endpoint":       true,
			"sql_endpoint":         true,
			"anomaly_endpoint":     true,
			"experimental_kprobes": false,
		},
		"_echo_label": s.cfg.label,
	})
}

// /api/v1/health (bearer-required).
func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !s.requireBearer(w, r) {
		return
	}
	s.write(w, r, http.StatusOK, map[string]any{
		"status":      "ok",
		"version":     "v1.0.0",
		"_echo_label": s.cfg.label,
	})
}

// /api/v1/tools/list (bearer-required, ACL-filtered in real Echo; we
// always return the full set since we don't model tenant scopes).
func (s *server) handleToolsList(w http.ResponseWriter, r *http.Request) {
	if !s.requireBearer(w, r) {
		return
	}
	tools := []map[string]any{
		{
			"name":        "fleet.cluster.summary",
			"description": "Cluster-wide GPU summary.",
			"input_schema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			"output_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"rows": map[string]any{"type": "array"},
				},
			},
		},
		{
			"name":        "fleet.cluster.anomaly_list",
			"description": "Recent anomalies.",
			"input_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"time_window":     map[string]any{"type": "string"},
					"severity_filter": map[string]any{"type": "string"},
					"limit":           map[string]any{"type": "integer"},
					"cluster_id":      map[string]any{"type": "string"},
				},
			},
		},
	}
	s.write(w, r, http.StatusOK, map[string]any{
		"tools":       tools,
		"_echo_label": s.cfg.label,
	})
}

// /api/v1/whoami (bearer-required).
func (s *server) handleWhoami(w http.ResponseWriter, r *http.Request) {
	if !s.requireBearer(w, r) {
		return
	}
	s.write(w, r, http.StatusOK, map[string]any{
		"bearer_id":   "fake-bearer-hash",
		"cluster_ids": []string{},
		"capabilities": map[string]bool{
			"sql":     true,
			"tools":   true,
			"anomaly": true,
		},
		"_echo_label": s.cfg.label,
	})
}

// /api/v1/openapi.json (bearer-required).
func (s *server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	if !s.requireBearer(w, r) {
		return
	}
	s.write(w, r, http.StatusOK, map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Ingero Echo (fake)",
			"version": "v1",
		},
		"paths":       map[string]any{},
		"_echo_label": s.cfg.label,
	})
}

// POST /api/v1/sql (bearer-required).
func (s *server) handleSQL(w http.ResponseWriter, r *http.Request) {
	if !s.requireBearer(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		s.write(w, r, http.StatusMethodNotAllowed, map[string]any{
			"error":       "method not allowed",
			"_echo_label": s.cfg.label,
		})
		return
	}
	s.write(w, r, http.StatusOK, map[string]any{
		"columns":     []string{"host", "events"},
		"rows":        [][]any{{"node-a", 123}, {"node-b", 456}},
		"_echo_label": s.cfg.label,
	})
}

// POST /api/v1/tools/<name> (bearer-required).
func (s *server) handleTool(w http.ResponseWriter, r *http.Request) {
	if !s.requireBearer(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		s.write(w, r, http.StatusMethodNotAllowed, map[string]any{
			"error":       "method not allowed",
			"_echo_label": s.cfg.label,
		})
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/tools/")
	if name == "" {
		s.write(w, r, http.StatusBadRequest, map[string]any{
			"error":       "tool name missing",
			"_echo_label": s.cfg.label,
		})
		return
	}
	// Two scripted responses; everything else returns 404.
	switch name {
	case "fleet.cluster.summary":
		s.write(w, r, http.StatusOK, map[string]any{
			"result": map[string]any{
				"rows": []map[string]any{
					{"host": "node-a", "ok": true, "_echo_label": s.cfg.label},
					{"host": "node-b", "ok": false, "_echo_label": s.cfg.label},
				},
			},
		})
	case "fleet.cluster.anomaly_list":
		s.write(w, r, http.StatusOK, map[string]any{
			"result": map[string]any{
				"rows": []map[string]any{
					{
						"ts":          time.Now().UTC().Format(time.RFC3339),
						"severity":    "warn",
						"summary":     "fake anomaly from " + s.cfg.label,
						"_echo_label": s.cfg.label,
					},
				},
			},
		})
	default:
		s.write(w, r, http.StatusNotFound, map[string]any{
			"error":       "no such tool",
			"code":        "tool_not_found",
			"name":        name,
			"_echo_label": s.cfg.label,
		})
	}
}

// muxRouter dispatches the small route table.
func (s *server) muxRouter() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/versions", s.handleVersions)
	mux.HandleFunc("/api/v1/health", s.handleHealth)
	mux.HandleFunc("/api/v1/tools/list", s.handleToolsList)
	mux.HandleFunc("/api/v1/whoami", s.handleWhoami)
	mux.HandleFunc("/api/v1/openapi.json", s.handleOpenAPI)
	mux.HandleFunc("/api/v1/sql", s.handleSQL)
	mux.HandleFunc("/api/v1/tools/", s.handleTool)
	return mux
}

func main() {
	cfg := parseFlags()
	s := &server{cfg: cfg, startedAt: time.Now()}
	if cfg.requestLogPath != "" {
		f, err := os.OpenFile(cfg.requestLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			log.Fatalf("open request log: %v", err)
		}
		defer f.Close()
		s.logFile = f
	}
	log.Printf("fake-echo %s listening on %s", cfg.label, cfg.addr)
	if err := http.ListenAndServe(cfg.addr, s.muxRouter()); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
