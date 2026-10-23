# Changelog

All notable changes to the `flowi` command line are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/) and the file is generated from Conventional Commits
by `git cliff` (see `cliff.toml`); `flowi changelog` prints it.

## [Unreleased]

### Features

- **send:** send a saved request with hooks, versions, retries and masked secrets
- **run:** run flows with live progress, batches, parallel branches and reports
- **serve:** replay saved responses as a fake API, run flows on HTTP calls and cron schedules
- **schedule:** add, list and remove cron schedules for `flowi serve`
- **import:** import Postman, Bruno, HAR and curl; export OpenAPI, Arazzo and Mermaid
- **http:** HTTP/3 with `http.version: 3`
- **otel:** export one OpenTelemetry trace per run when `otel.endpoint` is set
- **telemetry:** opt-in usage reporting, off by default, with `flowi telemetry show`
- **news:** show signed project announcements with `flowi news`
- **digest:** send run summaries to Slack, Discord, Teams or email-style webhooks
- **changelog:** `flowi changelog` shows what changed
