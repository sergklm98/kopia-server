# kopia-server

HTTP/gRPC server and server-side orchestration built on `kopia-lib`.

## Scope

- API route registration and request handling.
- Authentication, sessions, and CSRF behavior.
- Tasks, source management, repository connection, and server lifecycle.
- API-only mode and static frontend serving.

The server must not import `kopia-cli`.

## Frontend modes

- External frontend directory for development and normal system-browser hosting.
- Optional embedded frontend assets as a separate build mode.
- API-only mode for hosts such as Windows services and Android.

The server will define safe relative-path resolution, missing-asset behavior, index fallback, cache behavior, and asset security.

## Development status

The server has a public lifecycle package with API-only and external frontend directory modes. It opens configured repositories through `kopia-lib`, and `/api/v1/repo/status` follows the monolith's disconnected/direct-repository status behavior. The initial `/api/v1/cli` and `/api/v1/current-user` endpoints are also ported, and all are protected by the optional server authentication boundary. The executable is a thin signal-driven host, and the flow is covered by listener-level smoke tests.

The executable accepts `--address`, `--frontend-dir`, `--config`, `--password`, `--server-username`, and `--server-password`, with environment fallbacks for local hosting. The next milestone is porting the monolith's remaining authenticated UI and control API route registration, followed by tasks and source management.

## Make targets

Use `make check` for build, tests, and formatting verification. `make demo` runs the API/frontend and authentication smoke tests, and `make build` writes `bin/kopia-server`.
