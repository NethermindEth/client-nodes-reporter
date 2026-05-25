# client-nodes-reporter

A small Go CLI that:

1. Scrapes Ethereum execution-layer client distribution data (currently from [ethernodes.org](https://ethernodes.org)),
2. Records one row per run in a Notion database,
3. Posts a Slack message summarising today's count and a 35-day trend chart (rendered via QuickChart).

It is designed to be run as a one-shot job — locally, in a container, or as a Kubernetes CronJob.

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

## Running as a Kubernetes CronJob

The image's entrypoint is the `reporter` binary, so K8s passes CLI args via `args:` and configuration via environment variables (Secret + ConfigMap). One CronJob per client is the recommended pattern — the binary scrapes one client per invocation.

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: client-nodes-reporter-nethermind
spec:
  schedule: "0 10 * * *"   # 10:00 UTC daily
  concurrencyPolicy: Forbid
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 3
  jobTemplate:
    spec:
      backoffLimit: 2
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: reporter
              image: ghcr.io/nethermindeth/client-nodes-reporter:latest
              args:
                - --client=nethermind
                - --source=ethernodes
              envFrom:
                - secretRef:
                    name: client-nodes-reporter-secrets   # REPORTER_NOTION_TOKEN, REPORTER_SLACK_APP_TOKEN
                - configMapRef:
                    name: client-nodes-reporter-config    # REPORTER_NOTION_DB, REPORTER_SLACK_CHANNEL
```

Notes:
- The image is published under the org `nethermindeth` on GHCR. GHCR images are private by default; either flip visibility to public or attach `imagePullSecrets` referencing a GHCR PAT.
- The binary exits non-zero on any failure (Cobra propagates the error from `RunE`). `backoffLimit: 2` will retry twice before the Job is marked failed.
- The process responds to SIGTERM by cancelling its root context, so K8s can drain pods cleanly during deletes.

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
- No automated tests. Selector drift on ethernodes.org will only surface as a failed K8s Job; if the Slack message stops appearing, run the binary locally with `--debug` to see which step failed.
- The ethernodes scraper only requests gzip+deflate (not brotli) to avoid pulling in a brotli decoder. Cloudflare currently honours this; if it ever stops, add `github.com/andybalholm/brotli` and teach `readMaybeGzip` about `Content-Encoding: br`.
