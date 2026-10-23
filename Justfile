# Flow-I command-line tool. Run `just` to list recipes.

set shell := ["bash", "-euo", "pipefail", "-c"]

bin := justfile_directory() / "bin" / "flowi"

# Scratch area for trying commands: its own config dir, so your real Flow-I
# settings and workspaces are never touched.
sandbox := justfile_directory() / ".sandbox"
lib := justfile_directory() / ".." / "flowi-lib"
example := lib / "examples" / "user-creation"

# The sandbox vault uses a passphrase key, so trying commands never writes
# to your OS keychain.
sandbox_env := "FLOWI_VAULT_KEY=flowi-sandbox"

# Port the demo recipe runs the demo API on (the example's local
# environment says 8080; the demo points its copy here instead).
demo_port := "18080"

# List recipes.
default:
    @just --list

# Build the flowi binary into ./bin (no cgo, as released).
build:
    CGO_ENABLED=0 go build -o {{bin}} ./cmd/flowi

# Run flowi inside the sandbox, e.g. `just cli ls requests` or `just cli config list`.
cli *args: build
    @mkdir -p {{sandbox}}
    @cd {{sandbox}} && {{sandbox_env}} {{bin}} --config-dir {{sandbox}}/config {{args}}

# Serve the demo API the example collection talks to (default 127.0.0.1:8080, password "demo").
demo-server *args:
    cd {{lib}} && go run ./examples/demoserver/cmd/flowi-demo-server {{args}}

# Delete the sandbox (config, workspace and collections).
sandbox-reset:
    rm -rf {{sandbox}}

# Fresh sandbox with the example collection mounted, ready for `just cli`. For sends,
# run `just demo-server` in another terminal and `just cli secret set local password` (demo).
sandbox: build sandbox-reset
    @mkdir -p {{sandbox}}
    @cp -R {{example}} {{sandbox}}/user-creation
    @cd {{sandbox}} && {{sandbox_env}} {{bin}} --config-dir {{sandbox}}/config workspace mount ./user-creation
    @mkdir -p {{sandbox}}/config/workspaces/default/environments
    @cp {{sandbox}}/user-creation/environments/*.yaml {{sandbox}}/config/workspaces/default/environments/

# Walk through the main commands on a fresh sandbox, sending requests to the demo API.
demo: build sandbox-reset
    #!/usr/bin/env bash
    set -uo pipefail
    mkdir -p {{sandbox}}
    cp -R {{example}} {{sandbox}}/user-creation
    sed -i.bak 's#http://localhost:8080#http://127.0.0.1:{{demo_port}}#' {{sandbox}}/user-creation/environments/local.yaml
    rm {{sandbox}}/user-creation/environments/local.yaml.bak
    # Environments belong to the workspace, not to the collection: put the example's where the workspace reads them.
    mkdir -p {{sandbox}}/config/workspaces/default/environments
    cp {{sandbox}}/user-creation/environments/*.yaml {{sandbox}}/config/workspaces/default/environments/
    (cd {{lib}} && go build -o {{sandbox}}/demo-server ./examples/demoserver/cmd/flowi-demo-server) || exit 1
    {{sandbox}}/demo-server -addr 127.0.0.1:{{demo_port}} 2>{{sandbox}}/demo-server.log &
    server=$!
    trap 'kill $server 2>/dev/null; wait $server 2>/dev/null' EXIT
    cd {{sandbox}}
    export {{sandbox_env}}
    step() {
        printf '\n\033[1m$ flowi %s\033[0m\n' "$*"
        {{bin}} --config-dir {{sandbox}}/config "$@"
        local rc=$?
        [ $rc -eq 0 ] || printf '\033[2m(exit %d)\033[0m\n' $rc
    }
    step version
    step init ./scratch --name Scratch
    step workspace mount ./user-creation
    step workspace ls
    step ls requests
    step validate
    step fmt --check
    step config set timeout 45s
    step config explain timeout
    printf '\n\033[1m$ echo demo | flowi secret set local password\033[0m\n'
    echo demo | {{bin}} --config-dir {{sandbox}}/config secret set local password
    step secret ls
    step send auth/login --env local --with email=ada@example.com --prefer-version 2 --collection "User creation"
    # v1 login returns {data: {token, sub}}; the body is on stdout, the summary on stderr.
    login=$({{bin}} --config-dir {{sandbox}}/config send auth/login --env local --with email=ada@example.com --collection "User creation" 2>/dev/null)
    token=$(printf '%s' "$login" | sed -n 's/.*"token": "\([^"]*\)".*/\1/p' | head -1)
    sub=$(printf '%s' "$login" | sed -n 's/.*"sub": "\([^"]*\)".*/\1/p' | head -1)
    step send auth/verify --env local --with token="$token" --with subject="$sub" --with code=123456 --collection "User creation"
    printf '\n\033[1m$ flowi send auth/login --env local --output json | head -3\033[0m\n'
    {{bin}} --config-dir {{sandbox}}/config send auth/login --env local --with email=ada@example.com --output json --collection "User creation" | head -3
    step impact auth/login --collection "User creation"
    step cookies clear
    step run user-creation --env local --collection "User creation"
    step run onboarding-check --env local --collection "User creation" --quiet --report junit={{sandbox}}/onboarding.xml
    printf '\n\033[1m$ head -4 onboarding.xml\033[0m\n'
    head -4 {{sandbox}}/onboarding.xml
    printf '\nSandbox kept at %s. Try more with `just cli <command>` (start `just demo-server` for sends); `just sandbox-reset` clears it.\n' "{{sandbox}}"

# Run both example flows against the demo API on a fresh sandbox (live tree on a terminal), writing reports to the sandbox.
demo-run: build sandbox-reset
    #!/usr/bin/env bash
    set -uo pipefail
    mkdir -p {{sandbox}}
    cp -R {{example}} {{sandbox}}/user-creation
    sed -i.bak 's#http://localhost:8080#http://127.0.0.1:{{demo_port}}#' {{sandbox}}/user-creation/environments/local.yaml
    rm {{sandbox}}/user-creation/environments/local.yaml.bak
    # Environments belong to the workspace, not to the collection: put the example's where the workspace reads them.
    mkdir -p {{sandbox}}/config/workspaces/default/environments
    cp {{sandbox}}/user-creation/environments/*.yaml {{sandbox}}/config/workspaces/default/environments/
    (cd {{lib}} && go build -o {{sandbox}}/demo-server ./examples/demoserver/cmd/flowi-demo-server) || exit 1
    {{sandbox}}/demo-server -addr 127.0.0.1:{{demo_port}} 2>{{sandbox}}/demo-server.log &
    server=$!
    trap 'kill $server 2>/dev/null; wait $server 2>/dev/null' EXIT
    # Wait (up to 5s) until the server answers: the first run starts at once.
    for _ in $(seq 50); do curl -fs -o /dev/null http://127.0.0.1:{{demo_port}}/v1/config && break; sleep 0.1; done
    cd {{sandbox}}
    # The demo password comes from the environment, so no vault is needed.
    export {{sandbox_env}} FLOWI_SECRET_LOCAL_PASSWORD=demo
    flowi() { {{bin}} --config-dir {{sandbox}}/config "$@"; }
    flowi workspace mount ./user-creation >/dev/null
    for flow in user-creation onboarding-check; do
        printf '\n\033[1m$ flowi run %s --env local\033[0m\n' "$flow"
        flowi run "$flow" --env local --report junit={{sandbox}}/$flow.xml --report markdown={{sandbox}}/$flow.md
        rc=$?
        [ $rc -eq 0 ] || printf '\033[2m(exit %d)\033[0m\n' $rc
    done
    printf '\nReports: %s/*.xml, %s/*.md\n' "{{sandbox}}" "{{sandbox}}"

# Run every test (including the script tests against the real binary) with the race detector, then check the no-cgo build.
test-all:
    go test -race ./...
    CGO_ENABLED=0 go build ./...

# Run tests quickly (no race detector), e.g. `just test ./internal/app -run TestScript/fmt`.
test *args="./...":
    go test {{args}}

# Format code, tidy go.mod and verify downloaded modules.
clean:
    go fmt ./...
    go mod tidy
    go mod verify

# Lint with golangci-lint (v2).
lint:
    golangci-lint run ./...

# Everything CI checks: clean, lint, test-all.
check: clean lint test-all

# Regenerate CHANGELOG.md from Conventional Commits (needs git-cliff).
changelog:
    git cliff --output CHANGELOG.md
