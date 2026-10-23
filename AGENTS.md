# AGENTS.md: flowi-cli

Instructions for AI coding agents (and a useful checklist for people) working in this repository.
`flowi-cli` is the command-line tool of Flow-I. It only wires `flowi-lib` into commands: the logic lives there.
Read `README.md` for the commands and `../flowi-lib/docs/development.md` for how the repositories fit together.

## Musts

- **Run `just check` before you call work done.** It tidies, lints, and runs every test with the race detector,
  including the script tests against the real binary and a no-cgo build. Run it in the background and write the
  output to a file.
- **Never commit or push** unless the owner asks you to in that conversation.
- **Keep the binary free of cgo.** It must stay a single static binary on every platform.
- **Put logic in `flowi-lib`.** A command parses flags, calls one library method, and prints the result. If you
  need a new rule, add it to the library, with its test, and call it from here.
- **Keep the exit codes.** 0 success, 1 a flow, hook or assertion failed (or a check found differences), 2 invalid
  usage or input, 3 cancelled, 4 system error. A new failure picks one of these.
- **Write results to standard output and everything else to standard error**, so `flowi send … | jq` works.
  Every command that prints results also supports `--output json` (NDJSON events, then the result).
- **Put the SPDX header on every Go file** (`// SPDX-License-Identifier: AGPL-3.0-or-later`).
- **Wrap errors with context** and compare them with `errors.Is` / `errors.As`. Show users the diagnostic message
  and its hint, not a Go error string.

## Must nots

- **No cobra types or flags in `flowi-lib`**, and no business logic in `internal/cli`.
- **No global mutable state, no `init()`.** Build the dependency graph explicitly in the composition root
  (`internal/app`), with an injector created explicitly, never the package-level default.
- **No `os.Getenv`** outside the entry point: settings come through `flowi-lib`'s `settings` package.
- **No secrets** in output, logs, tests or fixtures. Secret prompts read from the terminal without echo.
  Values given by `--with` or `--input` are templates, not a place for credentials.
- **No `fmt.Print*` for logging.** Use `slog`. Printing a command's result to its writer is fine.

## Dos

- Keep commands thin and readable: orchestrators wire and pass errors on; workers hold small pieces of logic.
- Name functions with a verb that says what they do and return.
- Test commands by behaviour with the script tests (`testscript`, `*.txtar`) against the real binary, and with
  table-driven unit tests for the helpers. Use Arrange, Act, Assert with blank lines between.
- Use `just demo` to see the commands working in a throw-away sandbox (never your real settings), and
  `just cli <command>` to run one inside it.
- Update `README.md` and `.env.example` when you add a command or an environment variable (with a comment that
  says what it does).
- Releases and the changelog come from Conventional Commits (`feat:`, `fix:`, `docs:`…).

## Layout

| Path | What it holds |
|---|---|
| `cmd/flowi` | `main`: only starts the app |
| `internal/app` | composition root: builds the workspace and the commands |
| `internal/cli` | one file per command group (send, run, import/export, serve, config, secrets…) |
| `internal/tui` | the live progress views on a terminal |
