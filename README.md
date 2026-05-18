# Ingero for Grafana

A Grafana app plugin + datasource sub-plugin for Ingero GPU
observability. Bundles 11 reference dashboards covering NCCL
straggler triage, CUDA op profiling, memcpy bandwidth, memory
fragmentation, and throttle history; five starter alert rules; and a
native datasource that speaks to Ingero Echo's HTTP API for SQL, MCP
tool, and anomaly-stream queries.

Apache-2.0.

## What it ships

- **App plugin (`ingero-gpu-app`)**: nav entry, config page, and
  auto-provisioned dashboards.
- **Datasource sub-plugin (`ingero-gpu-datasource`)**: three query
  types against Ingero Echo's HTTP API:
  - **SQL** via `POST /api/v2/sql` (Echo's DuckDB store; read-only,
    60s timeout, 1GB memory cap, no filesystem builtins)
  - **MCP tool** via `POST /api/v2/tools/<name>` (server-validated
    against each tool's JSON schema)
  - **Anomaly stream** via `fleet.cluster.anomaly_list` with
    structured filters (time_window, severity, limit, cluster_id)
- **11 dashboards**: 5 cluster dashboards (overview, NCCL stragglers,
  memcpy bandwidth, memory fragmentation, per-node drilldown),
  4 single-host dashboards (trace overview, CUDA op profiler, data
  movement, memory throttle), 1 fleet pipeline-health operator
  dashboard, and 1 SQL reference dashboard.
- **5 starter alert rules** (all pure-Prometheus queries):
  - NCCL straggler suspected (cluster only)
  - GPU OOM imminent
  - GPU throttle sustained
  - Peer-relative MAD spike (cluster only)
  - Echo sink connection lost (cluster only)

## Architecture

The plugin connects to **Ingero Echo only**. Direct connections to
[agents](https://github.com/ingero-io/ingero) are not supported. Operators running a single-node deployment
install [Ingero Fleet](https://github.com/ingero-io/ingero-fleet)'s Echo component in standalone mode; the agent
remains the eBPF data producer, Echo remains the queryable surface.

The plugin's Go backend mediates every Echo request: bearer storage
lives in Grafana's secure store, the bearer never leaves the backend
process, and the backend exposes a cached `/resources/tools` endpoint
so the query editor can populate its tool picker without re-fetching
Echo on every form open.

## Compatibility

The plugin speaks the Ingero Echo HTTP API v2. On every datasource
connection it negotiates against `/api/versions`, pins the API
version for the session, and surfaces a clear error if the plugin
and the Echo endpoint have no API version in common. The README's
compatibility table is updated as new API versions ship.

## Installing

### From the Grafana Plugin Catalog

```
grafana-cli plugins install ingero-gpu-app
```

Restart Grafana; the App plugin auto-provisions its dashboards.

### From source (development)

```
git clone https://github.com/ingero-io/ingero-grafana-app
cd ingero-grafana-app
npm ci && npm run build
cd datasource && npm ci && npm run build && cd ..
```

Drop the build output into Grafana's `plugins/` directory or run
`docker compose up --build` to launch a dev Grafana with the plugin
and a development datasource provisioned.

## Configuring the datasource

In Grafana, **Connections → Add new connection → Ingero**.

| Field | Value |
|---|---|
| **Echo endpoint** | Base URL of the Echo HTTP API, e.g. `https://echo.internal:8081`. Do not include `/api/v2`; the backend appends paths. |
| **Bearer token** | Issued by your Echo operator. Stored in Grafana's secure store. |
| **Skip TLS verify** | DEV ONLY. The backend refuses to honor this on non-loopback endpoints. |

Click **Save & test**. Success returns `Echo <version> is reachable`.

### Multi-instance

Configure two or more Ingero datasources (one per Echo endpoint:
prod + staging, region-a + region-b, etc.). Each dashboard declares
an `$ingero_source` template variable; operators switch the
dashboard's Ingero-driven panels between configured datasources via
the variable dropdown. Prometheus-driven panels on the same dashboard
are unaffected by the switch.

## Provisioning the alerts

```
cp provisioning/alerting/ingero-gpu.yaml /etc/grafana/provisioning/alerting/
```

Replace the `${DS_PROMETHEUS}` placeholder in the YAML with the UID
of your Prometheus datasource (the one scraping the Ingero agent's
`:9090` endpoint, and for the cluster-only rules, the fleet/Echo
collector's `:9090`).

## Security posture

- **Bearer storage**: Grafana `secureJsonData`. Never read back to
  the frontend after save. Never logged. Plugin frontend uses
  `/api/v2/whoami`'s `bearer_id` field for any "logged in as" UX,
  never anything derived from the raw token.
- **Bearer rotation**: on a mid-session 401 the plugin marks the
  datasource as terminal-unauthenticated until the operator
  re-pastes a token; it does not retry-loop on a stale bearer.
- **TLS**: required by default. `insecureSkipVerify` is a per-
  instance UI toggle, honored only when the endpoint host is
  `127.0.0.1` / `localhost` / `::1`. The backend refuses to honor it
  on routable addresses and logs a warning on every request when
  honored.
- **Tools/list scoping**: Echo filters `/api/v2/tools/list` per the
  calling bearer, so a tenant-scoped bearer sees a smaller tool set.
  The plugin caches the list per `(datasource instance, bearer
  hash)` so a bearer change cannot serve a stale list.
- **`cluster_id` validation**: client-side
  `^[A-Za-z0-9_.-]{1,64}$` before forwarding to Echo.
- **`time_window` validation**: client-side `^[0-9hdms]{1,16}$`.

## Repository layout

```
ingero-grafana-app/
├── src/                    App plugin (TS frontend, plugin.json,
│   ├── dashboards/         dashboard JSONs)
│   └── components/         (ConfigEditor + AppConfig + App)
├── datasource/             Datasource sub-plugin
│   ├── src/                (TS query editor, types, datasource)
│   └── pkg/                (Go backend: echo client, query dispatch,
│                            tools/list cache, resource endpoints)
├── provisioning/
│   ├── alerting/           Five starter alert rules
│   └── plugins/            Dev provisioning for docker compose
├── tests/
│   ├── fake-echo/          Scripted Echo HTTP server for e2e
│   ├── alerts/             NCCL straggler scenario unit tests
│   └── *.spec.ts           Playwright e2e
└── .github/workflows/      CI + release
```

## Contributing

PRs welcome. The PR template lists the hard gates CI enforces
(no raw bearer / token references in any logger or `console.*` call,
no new filesystem or network access in `datasource/pkg/`, alert YAML
structural invariants, compat matrix kept in sync on version bumps).

## License

Apache-2.0. See [LICENSE](https://github.com/ingero-io/ingero-grafana-app/blob/main/LICENSE).
