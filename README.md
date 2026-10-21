# Orpbal

Created with [Better Fullstack](https://github.com/Marve10s/Better-Fullstack).

## Applications and resources

- **astro** (typescript, frontend): `apps/web`; part `frontend:typescript:astro`
- **gin** (go, backend): `apps/server`; part `backend:go:gin`
- **postgres** (universal, database): `packages/db`; part `database:universal:postgres`
- **docker-compose** (universal, containerOrchestration): `.`; part `containerorchestration:universal:docker-compose`
- **github-actions** (universal, continuousIntegration): `.`; part `continuousintegration:universal:github-actions`

## Setup

Install the SDKs for the selected languages before preparing dependencies. SwiftUI needs macOS, Xcode, and XcodeGen; Kotlin Android apps need a JDK and Android SDK; Flutter needs the Flutter SDK. Rust web apps also need the WebAssembly target and Trunk or Dioxus CLI.

JavaScript dependencies are installed at the workspace root. Each native application keeps its own toolchain. Native package scripts require `bash` on PATH; install Git Bash on Windows. Run the shell commands below in Bash.

If dependencies were not prepared during creation, run:

```sh
bun install
(cd apps/server && go mod tidy)
```

Copy each application's `.env.example` to `.env` when present and configure database credentials before starting database-backed services.

## Local development

Start the web app and backend together:

```sh
bun run dev
```

- Web (Astro SSR): `http://localhost:4321`
- API (Go Gin): `http://localhost:8080`, health endpoint `http://localhost:8080/health`

The web app proxies `/api/*` to `SERVER_URL` (default `http://localhost:8080`), so the browser only talks to the Astro origin.

### Go Gin

Run independently from the project root:

```sh
cd apps/server && go run cmd/server/main.go
```

## Environment

Copy `apps/server/.env.example` to `apps/server/.env` and set:

- `DATABASE_URL` - platform PostgreSQL connection string.
- `ORPBAL_ENCRYPTION_KEY` - base64-encoded 32 bytes (`openssl rand -base64 32`) used to encrypt managed database passwords. Keep it stable; changing it makes stored passwords unreadable.
- `ORPBAL_BOOTSTRAP_ADMIN_USERNAME` / `ORPBAL_BOOTSTRAP_ADMIN_PASSWORD` - creates the first platform admin when no users exist.
- `APP_ENV` - `development` or `production`; `production` marks the session cookie `Secure`.
- `APP_ORIGIN` - allowed browser origin for the API (default `http://localhost:4321`).
- `ORPBAL_SESSION_IDLE_TIMEOUT` / `ORPBAL_SESSION_ABSOLUTE_TIMEOUT` / `ORPBAL_SESSION_ROTATION_INTERVAL` / `ORPBAL_SESSION_GRACE_PERIOD` - optional session policy overrides (30m / 8h / 15m / 30s).

Copy `apps/web/.env.example` to `apps/web/.env` to change the Astro-to-API URL (`SERVER_URL`, default `http://localhost:8080`).

Sign in at `/login` with the bootstrap admin. All later users are created from the Users admin page.

## Docker Compose

Create `apps/server/.env` first (see above); Compose loads it for the API and derives the container database URL from `POSTGRES_USER`, `POSTGRES_PASSWORD`, and `POSTGRES_DB`.

```sh
docker compose up --build
```

- Web: `http://localhost:3001`
- API: `http://localhost:8080`
- PostgreSQL: `localhost:5432`

The API image runs as a non-root user and does not need CGO. Web and API have healthchecks; web starts after the API is healthy. Compose defaults `APP_ENV=development` and `APP_ORIGIN=http://localhost:3001`; override them in the shell for a production origin.

## Tests

```sh
bun run check-types
bun run test
bun run build

cd apps/server
go vet ./...
TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/Orpbal?sslmode=disable' go test ./...
```

Integration tests create isolated schemas and drop them afterwards. CI runs the same checks with a `postgres:16` service.

## Connections

- **Go Gin** (default client connection): `http://localhost:8080`, health endpoint `http://localhost:8080/health`.

Cross-language clients communicate over HTTP. Framework-specific clients such as tRPC are used only with compatible backends. Generated connection files identify the default backend; additional services retain their own paths and endpoints.
