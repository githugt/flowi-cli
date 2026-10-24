# flowi-cli

`flowi`, the command-line tool of **Flow-I**: send saved API requests, run flows in CI, import and
export API descriptions, and serve a fake API from your saved responses.

> Status: **pre-alpha**. Commands and the file format will change.

## Why a command line

The [desktop app](https://github.com/githugt/flowi) is where you build requests and flows. The CLI runs
the same collections where the app cannot: in CI, on a server, in a script, or on a schedule. All logic
lives in [flowi-lib](https://github.com/githugt/flowi-lib), so a flow behaves the same everywhere. The
CLI builds without cgo, so it is one static binary on every platform.

## Install

```bash
go install github.com/githugt/flowi-cli/cmd/flowi@latest
```

Needs Go 1.27 or later; the binary lands in `$(go env GOPATH)/bin`. Release builds for macOS, Linux and Windows are
attached to each [GitHub release](https://github.com/githugt/flowi-cli/releases).

To build from a clone instead:

```bash
git clone https://github.com/githugt/flowi-cli && cd flowi-cli
just build        # puts the binary in ./bin/flowi
```

Runs on macOS, Linux and Windows (Windows 10 or later: programs built with Go 1.21 or later do not run on Windows 7
or 8).

## Five-minute tour

```bash
flowi init ./my-api                          # create a collection and mount it
flowi workspace mount ../other-api           # or mount an existing one
flowi ls requests                            # what is in it
flowi secret set staging password            # prompts; the value goes to the encrypted vault
flowi secret get staging password --reveal   # prints it again (without --reveal: only whether it is set)
flowi send auth/login --env staging --with email=a@example.com
flowi run user-creation --env staging        # run a flow
flowi impact auth/login                      # who uses this request, and do they still fit it?
```

`flowi send` prints the response body on standard output and everything else on standard error, so
`flowi send auth/login | jq .token` works.

## Commands

| Command | What it does |
|---|---|
| `init`, `workspace mount/ls`, `ls`, `new` | Create and mount collections; list and create requests and flows (`flowi new flow team/chain --from two-calls`) |
| `send <request>` | Send one saved request: HTTP, gRPC, or a message (publish to or receive from Kafka, MQTT, NATS, SQS and more). `--env`, `--with name=value`, `--prefer-version`, `--pin` to keep the answer as an example |
| `run <flow>` | Run a flow with a live progress tree. `--input`, `--seed` to repeat generated values, `--report junit=report.xml` (also `json`, `markdown`, `text`) |
| `impact <request>` | List everything that uses a request, and whether each still fits its contract. Exits 1 if something is broken |
| `validate`, `fmt` | Check collections for errors; rewrite files in canonical form |
| `import <kind> <file>` | Bring in OpenAPI, AsyncAPI, `.proto`, Arazzo, Postman, Bruno, HAR, `.http` or curl (a URL works; `-` reads standard input), and Mermaid sketches |
| `export arazzo\|mermaid <flow>` | Write a flow as Arazzo 1.0 or as a Mermaid flowchart for docs |
| `serve` | One process for the replayer, HTTP flow triggers and cron schedules (below) |
| `schedule`, `env`, `secret`, `cookies`, `blueprint`, `config` | Cron schedules; environments; the secret vault; cookie jars; flow templates; settings (`flowi config explain timeout`) |

Every command takes `--output json` for NDJSON events followed by the result. Run `flowi <command> --help`
for the details.

### Import and export

Importers report each item with ✓, ⚠ or ✗ and show problems at their line and column. Credentials become
`{{secret.…}}` placeholders and are never written to a file. When items cannot be imported nothing is
written unless you pass `--partial`. Use `--dry-run` to list the files first.

### Serve: replayer, triggers, schedules

```bash
flowi serve                                   # replayer on 127.0.0.1:8525, schedules, triggers if a token exists
curl -X POST localhost:8525/v1/login          # answered from auth/login's pinned example
flowi serve --record https://api.example.com  # forward calls, save the real answers, and make requests of new calls
flowi schedule add nightly --flow user-creation --cron '0 3 * * *' --env staging
```

- **Replayer.** A call is matched to a request by method and path and answered from its pinned example or
  last saved response. Request headers such as `X-Flowi-Status: 401` or `X-Flowi-Latency: 800ms` steer it.
- **Triggers.** `POST /flows/<flow>/run` starts a flow (add `?wait=30s` to wait). Create the bearer token with
  `flowi serve token`; without one, triggers are off. The endpoint speaks plain HTTP, so keep it on
  localhost or put a TLS-terminating proxy in front; ten bad tokens from one address in a minute lock that
  address out (429) until the minute ends.
- **Schedules** are kept in `workspace.yaml`. Everything binds to localhost by default, and Ctrl-C finishes running work first.

## Environments and secrets

Environments belong to the workspace, not to a collection, so they are never committed. Secret values live
only in the workspace vault, whose key is kept in the OS keychain. On CI or a machine without a keychain, set
`FLOWI_VAULT_KEY` (any passphrase), or give each value as `FLOWI_SECRET_<ENV>_<NAME>`.
Any setting can be overridden with `FLOWI_<KEY>`; see [`.env.example`](.env.example).

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success (for `send`, an HTTP error status alone is not a failure) |
| 1 | A flow, hook or assertion failed, a check found differences, or `impact` found a broken consumer |
| 2 | Invalid usage, files or configuration (undefined variable, missing secret, …) |
| 3 | Cancelled or aborted |
| 4 | System error, including network failures and reports that could not be written |

## Development

```bash
just demo        # walk through the main commands in a throw-away sandbox, against the demo API
just cli ls requests   # run flowi inside that sandbox; your real settings are never touched
just check       # tidy, lint and test with the race detector
```

Optional extras (OpenTelemetry traces, usage reporting, signed announcements) are off until configured;
see [flowi-lib](https://github.com/githugt/flowi-lib).

## Licence

AGPL-3.0-or-later. See [`LICENSE`](LICENSE).
