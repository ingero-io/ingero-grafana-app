# fake-echo

Tiny scripted HTTP server that mimics the Ingero Echo HTTP API
surface used by this plugin's e2e tests. Not a re-implementation of
Echo; it returns scripted responses keyed by request shape and logs
every request to a JSONL file so tests can assert *which* fake-Echo
instance got hit.

## Build + run

```
cd tests/fake-echo
go build -o ../../bin/fake-echo .

../../bin/fake-echo \
    --addr 127.0.0.1:8081 \
    --bearer dev-bearer \
    --label echo-a \
    --request-log /tmp/echo-a.jsonl
```

Two instances side by side cover the multi-instance e2e case (one
plugin install, two datasource instances pointing at echo-a + echo-b,
dashboards switching between them via `$ingero_source`).

## Routes

All bearer-required except `/api/versions`:

| Method | Path | Notes |
|---|---|---|
| GET | `/api/versions` | major.minor version only |
| GET | `/api/v2/health` | bearer-required |
| GET | `/api/v2/tools/list` | returns the 2 scripted tools |
| GET | `/api/v2/whoami` | returns scripted bearer_id |
| GET | `/api/v2/openapi.json` | minimal OpenAPI 3.1 doc |
| POST | `/api/v2/sql` | 2-row scripted result |
| POST | `/api/v2/tools/<name>` | only `fleet.cluster.summary` + `fleet.cluster.anomaly_list` are scripted; everything else returns 404 |

Every response body carries an `_echo_label` field so tests can
assert which fake-Echo answered.

Every response carries an `X-Request-Id: fake-<label>-<unix-ns>`
header so the plugin's req_id error-correlation path is exercised.

## Request log shape

```
{"time":"2026-05-14T20:00:00Z","method":"POST","path":"/api/v2/tools/fleet.cluster.summary","bearer_present":true,"label":"echo-a","status":200}
```

One JSONL record per HTTP request, appended to the file passed via
`--request-log`. Playwright tests `tail -f` this file (or read it
after the test step) to assert request shape and instance affinity.
