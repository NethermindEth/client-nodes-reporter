# client-nodes-reporter

A small Go CLI that:

1. Scrapes Ethereum execution-layer client distribution data (currently from [ethernodes.org](https://ethernodes.org)),
2. Records one row per run in a local SQLite database,
3. Posts a Slack message summarising today's count and a 35-day trend chart (rendered via QuickChart).

It is designed to be run as a one-shot job — locally, in a container, or via any scheduler (GitHub Actions, Kubernetes CronJob, systemd timer, plain cron, etc.).

## Architecture

- `cmd/` — Cobra root command, flag parsing, wiring.
- `datasources/` — implementations of the `DataSource` interface that scrape upstream sites.
- `database/` — SQLite storage (`AddClientData`, `GetLatestData`, state-scheme snapshots) and the Notion CSV importer.
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
| `--skip-update` | — | `false` | skip scraping and the database write; only read history and post to Slack |
| `--db-path` | `REPORTER_DB_PATH` | — | **required** — SQLite database file; created and migrated on first use |
| `--slack-app-token` | `REPORTER_SLACK_APP_TOKEN` | — | Slack bot token (`xoxb-…`) from an app without token rotation. Set this **or** `--slack-refresh-token` |
| `--slack-refresh-token` | `REPORTER_SLACK_REFRESH_TOKEN` | — | Slack refresh token (`xoxe-1-…`) from an app with token rotation. See [Slack app](#slack-app) |
| `--slack-client-id` | `REPORTER_SLACK_CLIENT_ID` | — | required with `--slack-refresh-token` |
| `--slack-client-secret` | `REPORTER_SLACK_CLIENT_SECRET` | — | required with `--slack-refresh-token` |
| `--slack-channel` | `REPORTER_SLACK_CHANNEL` | — | **required** — channel name or ID |
| `--flaresolverr-url` | `REPORTER_FLARESOLVERR_URL` | — | optional — FlareSolverr v1 endpoint (e.g. `http://localhost:8191/v1`). When set, all ethernodes fetches are proxied through it. See [Cloudflare workaround](#cloudflare-workaround-flaresolverr). |
| `--max-retries` | — | `3` | maximum retry attempts per fetch |
| `--retry-delay` | — | `1s` | initial backoff between retries (doubled on each attempt) |

## Running locally — from source

Requires Go 1.25+.

```sh
export $(grep -v '^#' .env | xargs)
go run main.go --debug --client nethermind --source ethernodes
```

Use `--skip-update` to avoid writing to the database while still posting the Slack report (useful for chart-only re-runs).

## Running locally — Docker

The image runs as the distroless `nonroot` user (UID 65532), so the directory holding the database must be writable by it.

```sh
mkdir -p data && sudo chown 65532 data
docker build -t client-nodes-reporter:local .
docker run --rm --env-file .env -v "$PWD/data:/data" -e REPORTER_DB_PATH=/data/reporter.db \
  client-nodes-reporter:local --debug --client nethermind --source ethernodes
```

Or, with the published image:

```sh
docker run --rm --env-file .env -v "$PWD/data:/data" -e REPORTER_DB_PATH=/data/reporter.db \
  ghcr.io/nethermindeth/client-nodes-reporter:latest \
  --client nethermind --source ethernodes
```

## Storage

All history lives in one SQLite file (`--db-path`), with a table per report: `client_data` for the ethernodes/ethernets reports and `state_scheme_snapshots` for `nethermind-state-scheme`. With token rotation on, the current Slack token pair is kept there too, in `slack_tokens`. The schema is migrated automatically on start (tracked with `PRAGMA user_version`). The reports chart the last 35 rows, so the file has to survive between runs: keep it on a persistent volume and back it up like any other state.

Several report types can share one file, but SQLite needs a filesystem with working locks: don't put it on NFS or another network filesystem, and don't open it from more than one host.

### Importing history from Notion

Earlier versions stored rows in Notion. Export each Notion database as CSV (`•••` → `Export` → `Markdown & CSV`) and import it:

```sh
reporter import-notion-csv --db-path reporter.db --table client-data --timezone Europe/Madrid clients.csv
reporter import-notion-csv --db-path reporter.db --table state-scheme --timezone Europe/Madrid state-scheme.csv
```

Notion writes `Created time` in the exporting user's timezone without saying which, so pass that zone as `--timezone` (default `UTC`). Rows already in the database are skipped, so the import can be re-run.

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

The binary is a one-shot, so any scheduler that can run a container or a binary once a day works. The repo includes one example: `.github/workflows/ethernodes-scrape.yml`, a manually triggered GitHub Actions workflow that brings up a FlareSolverr service container next to the scraper and keeps the SQLite file in the Actions cache between runs. The cache is evicted after 7 days without use, so treat that example as a demo, not durable storage.

For other schedulers (Kubernetes CronJob, systemd timer, plain cron) the recipe is the same:

1. Make sure a FlareSolverr instance is reachable from wherever the binary runs (sidecar container, peer pod, separate host).
2. Provide `REPORTER_SLACK_CHANNEL` and the Slack credentials (see [Slack app](#slack-app)) via env or `.env`.
3. Point `REPORTER_DB_PATH` at a file on persistent storage (for a Kubernetes CronJob, a PersistentVolumeClaim with `fsGroup: 65532` on the pod).
4. Set `REPORTER_FLARESOLVERR_URL` to the FlareSolverr `/v1` endpoint.
5. Invoke the binary or the published image once per day.

## Slack app

The reporter posts with a bot token and needs the `chat:write` scope (plus `chat:write.public` to post in channels the bot has not joined). Give it its own Slack app: with token rotation on, an app's tokens cannot be shared with another service that also renews them.

**Without token rotation**, set `REPORTER_SLACK_APP_TOKEN` to the `xoxb-…` bot token.

**With token rotation** (`OAuth & Permissions` → opt in, or `settings.token_rotation_enabled: true` in the manifest), bot tokens expire every 12 h and each refresh token can be used only once:

1. Install the app and exchange its bot token for a refresh token once (an `xoxb-…` token can be exchanged only once, so keep the output):

   ```sh
   curl -s https://slack.com/api/oauth.v2.exchange \
     -d client_id="$CLIENT_ID" -d client_secret="$CLIENT_SECRET" -d token="xoxb-…"
   ```

2. Set `REPORTER_SLACK_REFRESH_TOKEN` to the returned `refresh_token`, plus `REPORTER_SLACK_CLIENT_ID` and `REPORTER_SLACK_CLIENT_SECRET` (`Basic Information` → `App Credentials`).

On each run the reporter takes the token pair from the `slack_tokens` table and renews it when less than an hour is left, saving the new pair before it is used. The env refresh token only starts the chain: it is used on the first run, or when it changes. To re-seed (after a reinstall, or if the stored refresh token was revoked), put a new refresh token in the env.

The database file then holds a live bot token (valid for up to 12 h) and the next refresh token, which cannot be used without the client secret. New database files are created with mode `0600`; keep the file, its volume and any backups as private as the client secret.

Every process that uses the same app must share the same database file. Two files would each spend the other's refresh tokens. Processes sharing one file are safe: the renewal runs inside a write transaction, so only one of them refreshes.

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

## Nethermind state-scheme report

`reporter nethermind-state-scheme` is a separate, Nethermind-specific subcommand that tracks how the Nethermind execution-layer fleet is moving from pre-v2 releases to v2, and on v2, how many nodes run the halfpath vs the flat state scheme. It reads node data from the public [enrscout](https://enrscout.ethnodeops.xyz) API, stores one row per run in the `state_scheme_snapshots` table, and posts a Slack summary table with a stacked bar chart of node counts per bucket over time.

Nodes are grouped by `client_version`:

| Bucket | Rule | Example |
|---|---|---|
| Pre V2 | major version < 2 (or unparseable) | `v1.39.3+28cbe2a0` |
| V2 Halfpath | v2+, ends in `-hp` | `v2.0.0+bec830cd-hp` |
| V2 Flat | v2+, ends in `-f` | `v2.0.0+bec830cd-f` |
| V2 Other | v2+, any other suffix | `v2.0.0+bec830cd-fit` |

Every node enrscout lists is counted. Nodes whose fingerprint is not fresh (`fp_status != ok`) are also counted separately as `Stale`. enrscout's methodology version is stored with each row, and the Slack message warns when it changes between runs.

```sh
REPORTER_DB_PATH=reporter.db go run main.go nethermind-state-scheme --debug --log-format text
```

The subcommand shares `--db-path`, `--slack-app-token`, `--slack-channel`, `--max-retries`, `--retry-delay`, `--skip-update`, `--debug` and `--log-format` with the root command, and adds:

| Flag | Env var | Default | Notes |
|---|---|---|---|
| `--network` | — | `mainnet` | `mainnet`, `hoodi` or `sepolia` |

Charts are uploaded via QuickChart's `/chart/create` endpoint, because the inline URL for this many series is longer than Slack's image URL limit.

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

- No automated tests for the scrapers. Selector drift on either source will only surface as a failed run; if the Slack message stops appearing, run the binary locally with `--debug` to see which step failed.
- Cloudflare can 403 direct requests from datacenter IPs against ethernodes.org. Route through FlareSolverr, or run from an egress Cloudflare considers benign. If FlareSolverr alone is not enough (IP-reputation block), the only remedy is to move the egress.
- The ethernodes scraper only requests gzip+deflate (not brotli) to avoid pulling in a brotli decoder. Cloudflare currently honours this; if it ever stops, add `github.com/andybalholm/brotli` and teach `readMaybeGzip` about `Content-Encoding: br`.
