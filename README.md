# client-nodes-reporter

A small Go CLI that:

1. Scrapes Ethereum execution-layer client distribution data (currently from [ethernodes.org](https://ethernodes.org)),
2. Records one row per run in a Notion database,
3. Posts a Slack message summarising today's count and a 35-day trend chart (rendered via QuickChart).

It is designed to be run as a one-shot job — locally, in a container, or as a GitHub Actions scheduled workflow.

## Architecture

- `cmd/` — Cobra root command, flag parsing, wiring.
- `datasources/` — implementations of the `DataSource` interface that scrape upstream sites.
- `database/` — Notion read/write (`AddClientData`, `GetLatestData`).
- `notifier/` — Slack message + QuickChart line graph.
- `configs/` — client-type enum (Nethermind, Geth, Besu, Erigon, Reth).

## Configuration

All settings can be provided as a flag or as an environment variable.

| Flag | Env var | Default | Notes |
|---|---|---|---|
| `--source`, `-s` | — | `ethernodes` | `ethernodes` or `ethernets` (the latter is currently broken; kept for reference) |
| `--client`, `-c` | — | `nethermind` | `nethermind`, `geth`, `besu`, `erigon`, `reth` |
| `--debug`, `-d` | — | `false` | sets log level to debug |
| `--log-format`, `-f` | `REPORTER_LOG_FORMAT` | `json` | `json` (recommended for K8s log shippers) or `text` |
| `--skip-update` | — | `false` | skip scraping and Notion write; only read history and post to Slack |
| `--notion-db` | `REPORTER_NOTION_DB` | — | **required** — Notion database ID |
| `--notion-token` | `REPORTER_NOTION_TOKEN` | — | **required** — Notion integration token |
| `--slack-app-token` | `REPORTER_SLACK_APP_TOKEN` | — | **required** — Slack bot token |
| `--slack-channel` | `REPORTER_SLACK_CHANNEL` | — | **required** — channel name or ID |
| `--max-retries` | — | `3` | retry knob (currently unused by scrapers) |
| `--retry-delay` | — | `1s` | retry knob (currently unused by scrapers) |

## Running locally — from source

Requires Go 1.23+.

```sh
export $(grep -v '^#' .env | xargs)
go run main.go --debug --client nethermind --source ethernodes
```

Use `--skip-update` to avoid writing to Notion while still posting the Slack report (useful for chart-only re-runs).

## Running locally — Docker

```sh
docker build -t client-nodes-reporter:local .
docker run --rm --env-file .env client-nodes-reporter:local \
  --debug --client nethermind --source ethernodes
```

Or, once published (see Releasing below):

```sh
docker run --rm --env-file .env \
  ghcr.io/nethermindeth/client-nodes-reporter:latest \
  --client nethermind --source ethernodes
```

## Running on a schedule (GitHub Actions)

The repo ships a workflow at `.github/workflows/ethernodes-scrape.yml` that runs the published image once per client per day (10:00 UTC, matrix over `nethermind`, `geth`, `besu`, `erigon`, `reth`) and can be triggered manually with a `skip-update` toggle.

This runs on GitHub-hosted runners on purpose: `ethernodes.org` sits behind Cloudflare, and Cloudflare currently blocks requests from common datacenter egress IPs (the AWS/OVH ranges used by self-hosted Kubernetes clusters in particular). The GitHub Actions runner IP ranges are the only egress we've found that reliably gets through without a paid residential proxy. Until that changes, running the binary on a schedule from anywhere other than GH Actions is not recommended.

The workflow expects these secrets to be set on the repo: `REPORTER_NOTION_DB`, `REPORTER_NOTION_TOKEN`, `REPORTER_SLACK_APP_TOKEN`. The Slack channel is hardcoded in the workflow.

## Data sources

### ethernodes (default)

- Total + per-client counts come from the main page at `https://ethernodes.org`.
- Per-client synced count comes from `https://ethernodes.org/client/el/<client>?synced=1`.
- Overall execution-layer synced count comes from `https://ethernodes.org/sync`.
- The site sits behind Cloudflare; the scraper uses browser-shaped headers and decompresses gzipped responses manually. If Cloudflare ever blocks the scraper, the run fails loudly (no silent fallbacks).

### ethernets

Currently broken. The data source is kept in the codebase but is no longer the default. Pass `--source ethernets` to attempt it.

## Adding a new client

1. Add a new `ClientType` constant in `configs/configs.go`.
2. Extend `ClientTypeFromString` and `ClientType.String` to handle the new value.
3. In `datasources/ethernodes.go`, extend `getClientURLName` (URL slug) and `matchesClientName` (display-name → enum match) for the new client.

## Releasing

Pushing to `main` or pushing a tag publishes to GHCR via `.github/workflows/docker-publish.yml`:

- Push to `main` → `ghcr.io/nethermindeth/client-nodes-reporter:latest` + `:main` + `:sha-<short>`
- Push a tag `vX.Y.Z` → `:X.Y.Z` + `:X.Y` (plus `latest`/`main`/`sha-` if on default branch)

For the workflow to push to GHCR, the repo must have **Settings → Actions → General → Workflow permissions → Read and write permissions** enabled.

## Known limitations / future work

- `--max-retries` and `--retry-delay` flags exist but are not wired through into the scrapers.
- The `ethernets` data source is broken and not currently being repaired.
- No automated tests. Selector drift on ethernodes.org will only surface as a failed workflow run; if the Slack message stops appearing, run the binary locally with `--debug` to see which step failed.
- Scraping from datacenter egress IPs (e.g. self-hosted Kubernetes on AWS/OVH) is currently blocked by Cloudflare. The GitHub Actions runner IPs are the only egress that reliably gets through; reviving a K8s deployment would require routing through a residential proxy or a Cloudflare-bypass service such as FlareSolverr.
- The ethernodes scraper only requests gzip+deflate (not brotli) to avoid pulling in a brotli decoder. Cloudflare currently honours this; if it ever stops, add `github.com/andybalholm/brotli` and teach `readMaybeGzip` about `Content-Encoding: br`.
