# Foglight

Open-source MCP server connecting AI agents to engineering infrastructure
(GitHub, Grafana, Datadog, Slack, Linear, Notion). Pre-launch; see
[README.md](README.md) for the pitch.

## Layout

- `cmd/foglight/` — Go binary entrypoint (currently a stub).
- `docs/` — Mintlify docs site, deploys to docs.foglight.co.
- `web/` — Cloudflare Workers landing page, deploys to foglight.co.

The three areas deploy independently; changes in one shouldn't require
changes in the others.

## Working here

- Go: `go build ./...` and `go run ./cmd/foglight`. Module path
  `github.com/eph5xx/foglight`, toolchain `go 1.26`.
- Web landing: from `web/`, preview with `wrangler dev`, deploy with
  `wrangler deploy`.
- Docs: from `docs/`, preview with `mint dev` (Mintlify CLI).

No tests or CI yet. Brand color is `#0088FF`.
