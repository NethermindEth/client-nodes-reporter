# client-nodes-reporter

A small Go CLI that:

1. Scrapes Ethereum execution-layer client distribution data (currently from [ethernodes.org](https://ethernodes.org)),
2. Records one row per run in a Notion database,
3. Posts a Slack message summarising today's count and a 35-day trend chart (rendered via QuickChart).

It is designed to be run as a one-shot job — locally, in a container, or via any scheduler (GitHub Actions, Kubernetes CronJob, systemd timer, plain cron, etc.).

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
| `--source`, `-s` | — | `ethernodes` | `ethernodes` or `ethernets` |
| `--client`, `-c` | — | `nethermind` | `nethermind`, `geth`, `besu`, `erigon`, `reth` |
| `--debug`, `-d` | — | `false` | sets log level to debug |
| `--log-format`, `-f` | `REPORTER_LOG_FORMAT` | `json` | `json` or `text` |
| `--skip-update` | — | `false` | skip scraping and Notion write; only read history and post to Slack |
| `--notion-db` | `REPORTER_NOTION_DB` | — | **required** — Notion database ID |
| `--notion-token` | `REPORTER_NOTION_TOKEN` | — | **required** — Notion integration token |
| `--slack-app-token` | `REPORTER_SLACK_APP_TOKEN` | — | **required** — Slack bot token |
| `--slack-channel` | `REPORTER_SLACK_CHANNEL` | — | **required** — channel name or ID |
| `--flaresolverr-url` | `REPORTER_FLARESOLVERR_URL` | — | optional — FlareSolverr v1 endpoint (e.g. `http://localhost:8191/v1`). When set, all ethernodes fetches are proxied through it. See [Cloudflare workaround](#cloudflare-workaround-flaresolverr). |
| `--max-retries` | — | `3` | maximum retry attempts per fetch |
| `--retry-delay` | — | `1s` | initial backoff between retries (doubled on each attempt) |

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

Or, with the published image:

```sh
docker run --rm --env-file .env \
  ghcr.io/nethermindeth/client-nodes-reporter:latest \
  --client nethermind --source ethernodes
```

## Cloudflare workaround (FlareSolverr)

`ethernodes.org` sits behind Cloudflare, which can return `HTTP 403` to direct requests from datacenter IPs or any client whose TLS fingerprint and header order don't look like a real browser. The scraper detects this and fails loudly rather than parsing garbage.

To work around it, the tool can route fetches through a [**FlareSolverr**](https://github.com/FlareSolverr/FlareSolverr) instance — an HTTP proxy that drives a real headless Chromium, which fixes both the TLS fingerprint and any JS challenges Cloudflare wants solved. Set `--flaresolverr-url` (or `REPORTER_FLARESOLVERR_URL`) to its `/v1` endpoint and all ethernodes fetches go through it; leave it empty and the tool does a direct fetch with browser-shaped headers (plus a `colly` fallback).

Caveat: FlareSolverr beats challenge-layer blocks (JS check, fingerprint, header order). It does **not** beat pure IP-reputation blocks — the FlareSolverr-issued request still leaves from the same egress IP as the host running it. If Cloudflare keeps serving 403 even with FlareSolverr in front, the IP is the problem and the host needs to be on an egress Cloudflare considers benign.

### Running locally with FlareSolverr

```sh
docker run -d --rm --name flaresolverr -p 8191:8191 ghcr.io/flaresolverr/flaresolverr:latest
# wait ~20s for Chromium to boot
export $(grep -v '^#' .env | xargs)
REPORTER_FLARESOLVERR_URL=http://localhost:8191/v1 \
  go run main.go --debug --client nethermind --source ethernodes
docker stop flaresolverr
```

## Running on a schedule

The binary is a one-shot, so any scheduler that can run a container or a binary once a day works. The repo includes one example: `.github/workflows/ethernodes-scrape.yml`, a GitHub Actions workflow that brings up a FlareSolverr service container next to the scraper and triggers the published image on a daily cron (also triggerable manually with a `skip-update` toggle).

For other schedulers (Kubernetes CronJob, systemd timer, plain cron) the recipe is the same:

1. Make sure a FlareSolverr instance is reachable from wherever the binary runs (sidecar container, peer pod, separate host).
2. Provide the four `REPORTER_*` secrets via env or `.env`.
3. Set `REPORTER_FLARESOLVERR_URL` to the FlareSolverr `/v1` endpoint.
4. Invoke the binary or the published image once per day.

## Data sources

### ethernodes (default)

- Total + per-client counts come from the main page at `https://ethernodes.org`.
- Per-client synced count comes from `https://ethernodes.org/client/el/<client>?synced=1`.
- Overall execution-layer synced count comes from `https://ethernodes.org/sync`.
- The site sits behind Cloudflare; see [Cloudflare workaround](#cloudflare-workaround-flaresolverr) above for how to route through FlareSolverr. If a fetch returns a Cloudflare challenge or block page, the run fails loudly (no silent fallbacks).

### ethernets

- Synced totals (network + per-client) come from `https://www.ethernets.io/?synced=yes`.
- Unsynced totals come from `https://www.ethernets.io/?synced=no`.
- The two are summed to produce the overall network total and per-client total; the synced page directly yields the synced counts.
- No Cloudflare in front of the site, so FlareSolverr is not needed.

## Adding a new client

1. Add a new `ClientType` constant in `configs/configs.go`.
2. Extend `ClientTypeFromString` and `ClientType.String` to handle the new value.
3. In `datasources/ethernodes.go`, extend `getClientURLName` (URL slug) and `matchesClientName` (display-name → enum match) for the new client.

## Releasing

Pushing to `main` or pushing a tag publishes to GHCR via `.github/workflows/docker-publish.yml`:

- Push to `main` → `:latest` + `:main` + `:sha-<short>`
- Push a tag `vX.Y.Z` → `:X.Y.Z` + `:X.Y` (plus `latest`/`main`/`sha-` if on default branch)

For the workflow to push to GHCR, the repo must have **Settings → Actions → General → Workflow permissions → Read and write permissions** enabled.

## Known limitations / future work

- No automated tests. Selector drift on either source will only surface as a failed run; if the Slack message stops appearing, run the binary locally with `--debug` to see which step failed.
- Cloudflare can 403 direct requests from datacenter IPs against ethernodes.org. Route through FlareSolverr, or run from an egress Cloudflare considers benign. If FlareSolverr alone is not enough (IP-reputation block), the only remedy is to move the egress.
- The ethernodes scraper only requests gzip+deflate (not brotli) to avoid pulling in a brotli decoder. Cloudflare currently honours this; if it ever stops, add `github.com/andybalholm/brotli` and teach `readMaybeGzip` about `Content-Encoding: br`.
