# Project Management Platform Backend (Jira-like)

Backend service for project/issue tracking with workflow rules, sprint planning, collaboration, search, and realtime updates.

## Architecture Overview

### High-level design
- **Architecture style**: Modular monolith (Go + Gin)
- **Primary datastore**: PostgreSQL (source of truth)
- **Realtime/event layer**: Redis + WebSocket
- **Deployment model**: Dockerized API with PostgreSQL/Redis dependencies

### Modules
- `internal/issues` - issue CRUD/update/transition, comments, watchers, notifications
- `internal/projects` - board state and project activity feed
- `internal/sprints` - sprint CRUD/lifecycle/carry-over
- `internal/search` - full-text + structured search
- `internal/notifications` - list/read notifications
- `internal/realtime` - websocket hub, pub/sub fan-out, replay, presence

### Key design decisions
- **Optimistic locking** on `issues.version` prevents silent overwrite under concurrent updates.
- **Workflow engine** enforces allowed transitions from `workflow_transitions`.
- **Redis + WebSocket** provides low-latency realtime board sync and missed-event replay (`since`).
- **Cursor pagination** used in activity/search for stable paging under frequent writes.

## Setup Instructions

### Prerequisites
- Docker + Docker Compose
- (Optional) Go 1.22+ for local non-container runs

### 1) Configure environment
```bash
cp .env.example .env
```

### 2) Start locally
```bash
docker compose up --build
```

### 3) Verify service
- Health: `http://localhost:8080/healthz`
- Swagger UI: `http://localhost:8080/swagger`
- OpenAPI spec: `http://localhost:8080/openapi.yaml`

### Migrations and seed
On API startup:
- `001_schema.sql` creates schema/indexes
- `002_seed.sql` inserts sample users/project/status/workflow
- `003_perf_indexes.sql` adds performance indexes

## Docker / Docker Compose (Local Development)

### Services (`docker-compose.yml`)
- `api` - Go backend service (port `8080`)
- `postgres` - PostgreSQL 16 (port `5432`)
- `redis` - Redis 7 (port `6379`)

### Notes
- API uses `DATABASE_URL` and Redis settings from environment.
- Data persists via Docker volume `pgdata`.
- Rebuilding API:
```bash
docker compose up --build api
```

## API Documentation

### Docs endpoints
- `GET /swagger` - interactive Swagger UI
- `GET /openapi.yaml` - OpenAPI schema document
- `GET /healthz` - health check

### Core endpoints (summary)

#### Issues / Workflow
- `POST /api/projects/:id/issues` - create issue
- `PATCH /api/issues/:id` - update issue fields (requires `version`)
- `POST /api/issues/:id/transitions` - workflow transition
- `GET /api/issues/:id/comments` - list comments
- `POST /api/issues/:id/comments` - add comment
- `PATCH /api/issues/:id/comments/:commentID` - update comment
- `DELETE /api/issues/:id/comments/:commentID` - delete comment
- `POST /api/issues/:id/watch?user_id=...` - watch issue
- `DELETE /api/issues/:id/watch?user_id=...` - unwatch issue

#### Projects / Activity
- `GET /api/projects/:id/board` - board grouped by status
- `GET /api/projects/:id/activity` - activity feed (cursor + filters)

#### Sprints
- `GET /api/projects/:id/sprints` - list sprints
- `POST /api/projects/:id/sprints` - create sprint
- `PATCH /api/sprints/:id` - update sprint
- `DELETE /api/sprints/:id` - delete sprint
- `POST /api/sprints/:id/start` - start sprint
- `POST /api/sprints/:id/complete` - complete sprint (velocity + carry-over)
- `POST /api/sprints/issues/move` - move issue between backlog/sprint

#### Search / Notifications
- `GET /api/search` - full-text + structured filters
- `GET /api/notifications?user_id=...` - list notifications
- `POST /api/notifications/:id/read` - mark notification read

#### Realtime
- `GET /ws/projects/:projectID?user_id=...&since=...`
  - Event stream includes issue/comment/sprint updates + presence

## Example Request/Response

### Create issue
```bash
curl -X POST "http://localhost:8080/api/projects/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa/issues" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "story",
    "title": "Add OAuth login",
    "description": "Implement OAuth flow",
    "priority": 2,
    "story_points": 5,
    "assignee_id": "11111111-1111-1111-1111-111111111111",
    "reporter_id": "22222222-2222-2222-2222-222222222222",
    "labels": ["auth", "backend"],
    "custom_fields": {}
  }'
```

Response:
```json
{
  "issue_id": "4ca3d42f-b1c8-4b3e-bf1e-7f2462db8c7f",
  "issue_key": "PROJ-12"
}
```

## Seeded Demo IDs

- Project: `aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa`
- User Jane: `11111111-1111-1111-1111-111111111111`
- User Bob: `22222222-2222-2222-2222-222222222222`
