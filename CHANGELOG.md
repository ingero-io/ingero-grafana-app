# Changelog

All notable changes to this project are documented here. The format
is loosely based on [Keep a Changelog](https://keepachangelog.com/);
the project follows [Semantic Versioning](https://semver.org/).

## [1.0.1]

### Fixed

- Datasource targets Echo API v2. Echo removed `/api/v1` in Fleet
  1.0 (HTTP 410 Gone); the datasource backend, settings, query
  editor, docs, and the bundled fake-echo test server now use
  `/api/v2`.

## [1.0.0]

First release.

### App plugin (`ingero-gpu-app`)

- Bundles 11 reference dashboards covering NCCL straggler triage,
  CUDA op profiling, memcpy bandwidth, memory fragmentation, and
  throttle history: 5 cluster dashboards (overview, NCCL stragglers,
  memcpy bandwidth, memory fragmentation, per-node drilldown),
  4 single-host dashboards (trace overview, CUDA op profiler, data
  movement, memory throttle), a fleet pipeline-health operator
  dashboard, and a SQL reference dashboard.
- Each dashboard declares an `$ingero_source` datasource template
  variable so a dashboard can be pointed at any configured Ingero
  datasource instance.
- Configuration page for selecting the Prometheus datasource and the
  Ingero datasource.

### Datasource plugin (`ingero-gpu-datasource`)

- Connects to Ingero Echo's HTTP+JSON API. Three query types:
  - **SQL** against Echo's DuckDB store (`POST /api/v1/sql`).
  - **MCP tool** invocation (`POST /api/v1/tools/<name>`), with the
    tool catalog discovered from `/api/v1/tools/list`.
  - **Anomaly stream** via the `fleet.cluster.anomaly_list` tool,
    with structured time-window / severity / limit / cluster filters.
- Query editor with a per-type form. The tool picker is populated
  from a cached `/resources/tools` endpoint on the plugin backend.
- Configuration: Echo endpoint URL, bearer token (stored in
  Grafana's secure store), and an optional dev-only TLS-skip toggle
  that the backend honors only for loopback endpoints.
- API version negotiation against `/api/versions`, pinned per
  session, with a clear error when the plugin and Echo versions do
  not overlap.
- Client-side validation of tool names, `cluster_id`, and
  `time_window` before a request leaves the browser.

### Alerting

- Five starter alert rules in `provisioning/alerting/ingero-gpu.yaml`:
  NCCL straggler suspected, GPU OOM imminent, GPU throttle sustained,
  peer-relative MAD spike, and Echo sink connection lost. All are
  Prometheus queries; operators substitute their Prometheus
  datasource UID at import time.

### Multi-instance

- One plugin install supports multiple Ingero datasources, each
  pointing at a separate Echo endpoint. Dashboards switch between
  them through the `$ingero_source` variable.
