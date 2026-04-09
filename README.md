# Project Management Platform Backend (Jira-like)

Production-focused modular monolith backend for project management with:
- Issue tracking and parent-child hierarchy
- Configurable workflows with transition guards
- Sprint lifecycle and velocity calculation
- Threaded comments, mentions, notifications, watchers
- Activity log/audit trail
- Full-text + structured issue search
- Real-time board updates and presence over WebSockets

## Architecture

- **Transport layer**: Gin REST API + WebSocket endpoint.
- **Domain layer**: `projects`, `issues`, `sprints`, `search`, `realtime`.
- **Data layer**: PostgreSQL (source of truth), Redis (pub/sub + replay buffer).
- **Concurrency strategy**: optimistic locking via `issues.version` to handle concurrent mutations.

## Stack

- Go 1.22
- Gin
- PostgreSQL + `pgx`
- Redis + `go-redis`
- Docker + Docker Compose

## Run Locally

```bash
cp .env.example .env
docker compose up --build
```

API runs at `http://localhost:8080`.

## Migrations and Seed Data

On startup the app auto-runs migrations:
- `001_schema.sql` creates all tables/indexes.
- `002_seed.sql` inserts sample users/project/status/workflow.

## API Endpoints

- `POST /api/projects/:id/issues` create issue
- `GET /api/projects/:id/board` board state grouped by status
- `PATCH /api/issues/:id` update issue fields (`version` required)
- `POST /api/issues/:id/transitions` transition issue status with workflow validation
- `GET /api/projects/:id/sprints` list sprints
- `POST /api/sprints/:id/start` start sprint
- `POST /api/sprints/:id/complete` complete sprint and return velocity + incomplete
- `GET /api/issues/:id/comments` list threaded comments
- `POST /api/issues/:id/comments` add comment (supports `@mentions`)
- `GET /api/projects/:id/activity` paginated activity stream (`cursor`)
- `GET /api/search?q=...` full-text + structured filtering
- `POST /api/issues/:id/watch?user_id=...` watch issue
- `DELETE /api/issues/:id/watch?user_id=...` unwatch issue
- `GET /ws/projects/:projectID?user_id=...&since=<event_id>` realtime stream + replay

## WebSocket Events

- `issue_created`
- `issue_updated`
- `issue_moved`
- `comment_added`
- `presence`

## Scenario Coverage

1. **Concurrent issue updates**: optimistic locking with `version`; stale update returns `409`.
2. **Sprint completion carry-over**: `/api/sprints/:id/complete` returns incomplete and supports `carry_over_issue_ids`.
3. **Workflow violation**: invalid transition returns `422` with `allowed_transitions`.

## Suggested Next Improvements

- Add auth + permissions
- Add OpenAPI/Swagger UI
- Extend transition hooks and custom field validators
- Add integration tests and load test profile
