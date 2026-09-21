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

Repository initialized for the Kopia multi-repository split. Source extraction has not started.

The first milestone is an independently buildable server consuming `kopia-lib`, serving the existing UI build from a sibling directory, and exposing a versioned management API.
