# Contributing to ingero-grafana-app

Pull requests welcome. This page covers the dev loop, the CI gates
your PR will hit, and what to put in the PR description.

## Dev loop

### One-time setup

```
npm ci
cd datasource && npm ci && cd ..
```

Node 22+ and Go (whatever `datasource/go.mod` declares) are
required. Both lockfiles are committed, so the installs are
deterministic.

### Watch + reload against a local Grafana

```
docker compose up --build
```

This builds the plugin's app + datasource bundles, builds two
fake-Echo HTTP servers (`tests/fake-echo/`), and mounts everything
into a Grafana 12 container at `http://localhost:3000`. Default
admin / admin. Two Ingero datasources are auto-provisioned at
boot, one per fake-Echo, named `Ingero echo-a` and `Ingero echo-b`.

For frontend-only changes:

```
npm run dev               # app plugin
cd datasource && npm run dev   # datasource plugin
```

Each runs webpack in watch mode; Grafana picks up the rebuilt
bundle on the next page refresh.

### Run the suites locally

```
# Frontend typecheck + lint + jest
npm run typecheck && npm run lint && npm run test:ci
cd datasource && npm run typecheck && npm run lint && npm run test:ci && cd ..

# Datasource Go backend
cd datasource && go vet ./... && go test -race ./... && cd ..

# Alert-rule scenario tests
python3 -m unittest tests/alerts/test_nccl_straggler.py
```

### Playwright e2e

```
npm run e2e
```

Requires the full docker-compose stack up (the fake-Echo containers
back the multi-instance tests).

## CI gates your PR must pass

`.github/workflows/ci.yml` runs five jobs in parallel:

1. **Pre-merge security + provisioning gates**: a bearer-leak grep
   over Go + frontend log calls, a check that the datasource
   backend does no filesystem or arbitrary network access, alert
   YAML structural validation, and the NCCL straggler scenario
   unit tests.
2. **Datasource Go backend**: `go vet` + `go test -race`.
3. **Datasource TypeScript**: typecheck, lint, jest.
4. **Build, lint and unit tests**: root frontend, signed-plugin
   validator.
5. **Playwright e2e**: a matrix across several Grafana versions.

A `Release` workflow triggers on `v*` tags and builds + signs the
plugin bundle.

## PR checklist

The pull-request template captures the per-PR gates:

- Tests cover new behaviour (or the absence is justified).
- `npm run lint` and `npm run typecheck` pass locally on both root
  and `datasource/`.
- No raw bearer / token references in any logger or `console.*`
  call. CI catches Go and TS surfaces.
- The datasource backend does no filesystem access or arbitrary
  network egress; only the configured Echo endpoint via the SDK
  HTTP client.
- CHANGELOG entry added.

## Architecture decisions of record

- **Plugin connects to Ingero Echo only.** Direct connections to
  Ingero agents are not supported. Reasoning: smaller attack
  surface, single auth + ACL surface, single audit log. See
  README "Architecture" section.
- **All three v0.1.0 query types route through the Go backend.**
  The frontend never talks to Echo directly; bearer stays in the
  Grafana secure store and reaches Echo through the plugin's Go
  http.Client.
- **Per-tool input/output schemas come from Echo, not bundled
  with the plugin.** The frontend reads them via the cached
  `/resources/tools` endpoint and renders forms off the schema.
  Schema changes on the server are picked up without a plugin
  release.

## Reporting issues

GitHub issues on this repo. Include:
- Plugin version (in Grafana, Plugins → Ingero → Version).
- Grafana version + edition.
- Ingero Fleet / Echo version (from `GET /api/versions` on your
  Echo endpoint, or the `Echo <version> is reachable` text in
  the datasource Save & test result).
- Steps to reproduce. If a panel is rendering wrong, the panel's
  JSON via Grafana's `Inspect → Panel JSON` is the most useful
  attachment.

## License

Apache-2.0.
