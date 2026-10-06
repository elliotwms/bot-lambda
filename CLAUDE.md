# bot-lambda

A small Go library for running Discord interaction bots on AWS Lambda. [Pinbot](https://github.com/elliotwms/pinbot-lambda) is the main user, so check a change against it before releasing.

## Layout

- `endpoint.go`: `Endpoint`, the Lambda handler.
  - `HandleRequest` is for function URLs and `HandleEvent` for API Gateway. Both go through `handle`, which verifies the ed25519 signature and then routes the interaction.
  - An empty public key **skips verification**. That's only meant for tests, and callers must reject it in production (Pinbot does in `main.go`).
  - With `WithDeferredResponseEnabled`, the deferred, ephemeral response goes out before the session provider runs, so a slow token fetch can't miss Discord's 3-second deadline.
- `sessionprovider/`: where the handler's `*discordgo.Session` comes from.
  - `ParamStore` reads the bot token through the Parameters and Secrets Lambda Extension.
  - `Cached` keeps only successful sessions, so errors are retried.
  - `Static` returns a fixed session.
- Routing and handler types come from [elliotwms/bot](https://github.com/elliotwms/bot) (`interactions/router`).

## Commands

```sh
go vet ./...
go test -race ./...
golangci-lint run
```

Tests run against [fakediscord](https://github.com/elliotwms/fakediscord) as a Go package and `httptest` servers, so they need no Docker.

Tracing uses the OpenTelemetry API only, through `internal/tracing`, with the global tracer provider. Applications choose the SDK and exporter, so don't add an SDK or exporter dependency outside tests.

## Conventions

- Commit messages and PR titles use [Conventional Commits](https://www.conventionalcommits.org). Merging to `main` runs `release.yml`, which tags a semver release worked out from them. PRs are squash-merged, so the PR title decides the version bump.
- This is a library, so treat exported API changes as breaking (`feat!:`), including option and provider signatures. Update the README example to match.
- Dependabot patch and minor updates are approved and auto-merged by `dependabot_reviewer.yml`, using a GitHub App token whose credentials come from [elliotwms/infra](https://github.com/elliotwms/infra).
- After a release, bump it in `pinbot-lambda`, or let Dependabot do it.
