# Implementation Plans

Generated from improve audit on 2026-06-30 at commit `fc8e5f5`. Executing via parallel subagents.

## Execution order & status

| Plan | Title | Agent | Status |
|------|-------|-------|--------|
| 001 | Security hardening | go-backend | DONE |
| 002 | Manager perf + correctness | go-backend | DONE |
| 003 | Characterization + integration tests | go-backend | DONE |
| 004 | Stremio security + CI | stremio-ci | DONE |
| 005 | DX + docs bundle | dx-docs | DONE |
| 006 | S3 object storage | go-backend + settings-ui + tests-docs | DONE |

## Verification gates (all agents)

- `go vet ./...` → exit 0
- `go test ./...` → exit 0
- `go test -tags=integration ./test/integration/...` → exit 0
- `cd web/dashboard && npm run build` → exit 0
- Stremio: `node --check index.js` and all `lib/*.js`
