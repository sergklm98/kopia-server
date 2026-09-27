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

## Make targets

Use `make check` for build, tests, and formatting verification. `make demo` runs the API/frontend and authentication smoke tests, and `make build` writes `bin/kopia-server`.
