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

- **Transport layer**: Gin REST API + WebSocket endpoint
- **Domain layer**: `projects`, `issues`, `sprints`, `search`, `realtime`, `notifications`
- **Data layer**: PostgreSQL (source of truth), Redis (pub/sub + replay buffer)
- **Concurrency strategy**: optimistic locking via `issues.version`

### Why this architecture

- **Modular monolith first**: faster delivery for an SDE-1 scope while keeping domain boundaries clean for future extraction.
- **PostgreSQL as source of truth**: relational integrity is critical for workflows, parent-child issue links, and auditable history.
- **Redis for realtime fan-out**: decouples HTTP mutation path from websocket subscribers and enables replay on reconnect.
- **Optimistic locking on issues**: low coordination overhead and explicit conflict handling for concurrent edits.
- **Cursor-based pagination**: stable ordering under write load for activity/search feeds.
- **Schema + app validation split**: DB constraints enforce invariants, handlers provide user-friendly errors and guardrails.

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

API base URL: `http://localhost:8080`

## Docs

- Swagger UI: `http://localhost:8080/swagger`
- OpenAPI YAML: `http://localhost:8080/openapi.yaml`

## Migrations and Seed Data

On startup the app auto-runs migrations:
- `001_schema.sql` creates tables and indexes
- `002_seed.sql` inserts sample users/project/status/workflow

## Data Types and Validation Rules

- IDs are UUID strings unless noted otherwise.
- Optional UUID fields can be omitted, set to `null`, or set to a valid UUID.
- Sending a non-UUID value (for example `"string"`) to UUID fields returns `400`.
- Date fields use `YYYY-MM-DD`.
- `issues.type` allowed values: `epic`, `story`, `task`, `bug`, `sub-task`.
- `sprints.status` allowed values: `planned`, `active`, `completed`.

## Health and Docs Endpoints

### `GET /healthz`
Health check.

**Response 200**
```json
{"ok": true}
```

### `GET /openapi.yaml`
Returns API spec YAML.

### `GET /swagger`
Serves Swagger UI.

## Issue Endpoints

### `POST /api/projects/:id/issues`
Create an issue in a project.

**Path params**
- `id` (UUID): project ID

**Body**
```json
{
  "type": "story",
  "title": "Add OAuth login",
  "description": "Implement Google OAuth",
  "priority": 2,
  "story_points": 5,
  "assignee_id": "7e5f9e7e-5e5f-4c6e-b65a-3ab0b2f8f9f1",
  "reporter_id": "8d6d09dc-d8f3-4265-960e-a312bf4f3d2a",
  "parent_id": null,
  "sprint_id": null,
  "labels": ["auth", "backend"],
  "custom_fields": {}
}
```

**Required fields**
- `type`, `title`

**Success 201**
```json
{
  "issue_id": "4ca3d42f-b1c8-4b3e-bf1e-7f2462db8c7f",
  "issue_key": "PROJ-12"
}
```

**Common errors**
- `400` invalid payload / invalid UUID
- `404` project not found
- `422` project has no statuses, custom field validation failure, or DB constraint error

---

### `PATCH /api/issues/:id`
Partially update issue fields with optimistic locking.

**Path params**
- `id` (UUID): issue ID

**Body**
```json
{
  "version": 1,
  "title": "Add OAuth login flow",
  "description": "Updated description",
  "priority": 1,
  "assignee_id": "7e5f9e7e-5e5f-4c6e-b65a-3ab0b2f8f9f1",
  "story_points": 8,
  "labels": ["auth", "api"],
  "sprint_id": "f4a6f884-cdb8-4365-8e9c-49ff5e3f0bd2",
  "custom_fields": {},
  "actor_id": "8d6d09dc-d8f3-4265-960e-a312bf4f3d2a"
}
```

**Required fields**
- `version` (> 0)

**Success 200**
```json
{"status": "updated"}
```

**Common errors**
- `400` invalid payload / version missing / invalid UUID
- `404` issue not found
- `409` stale `version` (conflict)
- `422` DB or custom field validation errors

---

### `POST /api/issues/:id/transitions`
Transition an issue to another status using workflow rules.

**Path params**
- `id` (UUID): issue ID

**Body**
```json
{
  "to_status": "In Review",
  "actor_id": "8d6d09dc-d8f3-4265-960e-a312bf4f3d2a"
}
```

**Success 200**
```json
{"status": "transitioned"}
```

**Common errors**
- `400` missing `to_status`
- `404` issue not found
- `422` status not found, invalid workflow transition, transition guard failure

---

### `GET /api/issues/:id/comments`
List issue comments (threaded via `parent_id`).

**Path params**
- `id` (UUID): issue ID

**Success 200**
```json
{
  "comments": [
    {
      "id": "5d7fd3c5-6faf-4baa-964b-17fc5888e98b",
      "parent_id": null,
      "content": "Looks good",
      "created_at": "2026-04-09T10:20:30Z",
      "user": {
        "user_id": "7e5f9e7e-5e5f-4c6e-b65a-3ab0b2f8f9f1",
        "display_name": "Jane Doe"
      }
    }
  ]
}
```

---

### `POST /api/issues/:id/comments`
Add a comment. Mentions like `@jane` create notifications.

**Path params**
- `id` (UUID): issue ID

**Body**
```json
{
  "user_id": "7e5f9e7e-5e5f-4c6e-b65a-3ab0b2f8f9f1",
  "content": "Please review this @jane",
  "parent_id": null
}
```

**Success 201**
```json
{"comment_id": "5d7fd3c5-6faf-4baa-964b-17fc5888e98b"}
```

**Common errors**
- `400` invalid payload
- `404` issue not found
- `422` DB constraint error

---

### `PATCH /api/issues/:id/comments/:commentID`
Update comment content (author only).

**Body**
```json
{
  "user_id": "7e5f9e7e-5e5f-4c6e-b65a-3ab0b2f8f9f1",
  "content": "Edited comment content"
}
```

**Success 200**
```json
{"status": "updated"}
```

---

### `DELETE /api/issues/:id/comments/:commentID`
Delete comment (author only).

**Body**
```json
{
  "user_id": "7e5f9e7e-5e5f-4c6e-b65a-3ab0b2f8f9f1"
}
```

**Success 200**
```json
{"status": "deleted"}
```

---

### `POST /api/issues/:id/watch?user_id=<uuid>`
Watch an issue.

**Success 200**
```json
{"status": "watching"}
```

### `DELETE /api/issues/:id/watch?user_id=<uuid>`
Unwatch an issue.

**Success 200**
```json
{"status": "unwatched"}
```

## Project Endpoints

### `GET /api/projects/:id/board`
Get board columns with issues grouped by status.

**Path params**
- `id` (UUID): project ID

**Success 200**
```json
{
  "project_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
  "columns": [
    {
      "id": "fcfcb905-9f11-41c9-b0e8-566ab30595af",
      "name": "To Do",
      "category": "todo",
      "issues": [
        {
          "id": "4ca3d42f-b1c8-4b3e-bf1e-7f2462db8c7f",
          "issue_key": "PROJ-12",
          "title": "Add OAuth login",
          "priority": 2,
          "story_points": 5,
          "updated_at": "2026-04-09T10:20:30Z"
        }
      ]
    }
  ]
}
```

---

### `GET /api/projects/:id/activity?cursor=<id>`
Get project activity feed (cursor pagination + filters).

**Path params**
- `id` (UUID): project ID

**Query params**
- `cursor` (optional, activity row ID)
- `event_type` (optional, exact event type; case-insensitive)
- `actor_id` (optional UUID)
- `issue_id` (optional UUID)
- `limit` (optional integer, `1..200`, default `50`)

**Success 200**
```json
{
  "events": [
    {
      "id": 101,
      "issue_id": "4ca3d42f-b1c8-4b3e-bf1e-7f2462db8c7f",
      "actor_id": "8d6d09dc-d8f3-4265-960e-a312bf4f3d2a",
      "event_type": "issue_created",
      "payload": {"title": "Add OAuth login"},
      "created_at": "2026-04-09T10:20:30Z"
    }
  ],
  "next_cursor": 101
}
```

## Sprint Endpoints

### `GET /api/projects/:id/sprints`
List sprints in a project.

**Success 200**
```json
{
  "sprints": [
    {
      "id": "f4a6f884-cdb8-4365-8e9c-49ff5e3f0bd2",
      "name": "Sprint 14",
      "start_date": "2026-04-01",
      "end_date": "2026-04-14",
      "status": "planned",
      "created_at": "2026-04-01T09:00:00Z"
    }
  ]
}
```

---

### `POST /api/projects/:id/sprints`
Create sprint.

**Body**
```json
{
  "name": "Sprint 14",
  "start_date": "2026-04-01",
  "end_date": "2026-04-14"
}
```

**Success 201**
```json
{
  "sprint_id": "f4a6f884-cdb8-4365-8e9c-49ff5e3f0bd2",
  "status": "planned"
}
```

**Common errors**
- `400` invalid payload or invalid date format
- `422` DB error

---

### `PATCH /api/sprints/:id`
Update sprint fields.

**Body (all optional)**
```json
{
  "name": "Sprint 14 (updated)",
  "start_date": "2026-04-02",
  "end_date": "2026-04-15",
  "status": "active"
}
```

**Success 200**
```json
{"status": "updated"}
```

---

### `DELETE /api/sprints/:id`
Delete sprint and detach issues from it (`issues.sprint_id = null`).

**Success 200**
```json
{"status": "deleted"}
```

---

### `POST /api/sprints/:id/start`
Mark sprint as active.

**Success 200**
```json
{"status": "active"}
```

---

### `POST /api/sprints/:id/complete`
Complete sprint and optionally carry incomplete issues to another sprint.

**Body (optional)**
```json
{
  "new_sprint_id": "f4a6f884-cdb8-4365-8e9c-49ff5e3f0bd2",
  "carry_over_issue_ids": [
    "4ca3d42f-b1c8-4b3e-bf1e-7f2462db8c7f"
  ]
}
```

**Success 200**
```json
{
  "status": "completed",
  "velocity_completed_points": 13,
  "incomplete_items": [
    {
      "issue_id": "4ca3d42f-b1c8-4b3e-bf1e-7f2462db8c7f",
      "issue_key": "PROJ-12",
      "story_points": 5
    }
  ]
}
```

---

### `POST /api/sprints/issues/move`
Move issue to sprint backlog/current sprint.

**Body**
```json
{
  "issue_id": "4ca3d42f-b1c8-4b3e-bf1e-7f2462db8c7f",
  "sprint_id": "f4a6f884-cdb8-4365-8e9c-49ff5e3f0bd2"
}
```

Set `sprint_id` to `null` to move issue out of a sprint.

**Success 200**
```json
{"status": "moved"}
```

## Search Endpoints

### `GET /api/search`
Full-text search across issue title/description and comments, with filters.

**Query params**
- `q` (required): query text
- `status` (optional): status name (case-insensitive)
- `assignee` (optional): assignee display name (case-insensitive)
- `priority_min` (optional, default `0`)
- `cursor` (optional, timestamp for pagination)

**Example**
```bash
curl "http://localhost:8080/api/search?q=oauth&status=To%20Do&priority_min=2"
```

**Success 200**
```json
{
  "results": [
    {
      "issue_id": "4ca3d42f-b1c8-4b3e-bf1e-7f2462db8c7f",
      "issue_key": "PROJ-12",
      "title": "Add OAuth login",
      "description": "Implement Google OAuth",
      "priority": 2,
      "updated_at": "2026-04-09T10:20:30Z"
    }
  ],
  "next_cursor": "2026-04-09T10:20:30Z"
}
```

## Notifications Endpoints

### `GET /api/notifications?user_id=<uuid>`
List latest notifications (max 100) for user.

**Success 200**
```json
{
  "notifications": [
    {
      "id": "de4fa94f-2ca3-4250-8af7-5e468cf8ca4b",
      "project_id": "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
      "issue_id": "4ca3d42f-b1c8-4b3e-bf1e-7f2462db8c7f",
      "type": "mention",
      "payload": {"mention": "jane"},
      "is_read": false,
      "created_at": "2026-04-09T10:20:30Z"
    }
  ]
}
```

### `POST /api/notifications/:id/read`
Mark notification as read.

**Success 200**
```json
{"status": "read"}
```

## WebSocket Endpoint

### `GET /ws/projects/:projectID?user_id=<uuid>&since=<event_id>`
Realtime project events stream.

- Protocol: WebSocket
- Use `ws://localhost:8080/ws/projects/<project_id>?user_id=<user_id>`
- `since` is optional and replays events after that event ID

Emitted events include:
- `issue_created`
- `issue_updated`
- `issue_moved`
- `comment_added`
- `comment_updated`
- `comment_deleted`
- `sprint_updated`
- `presence`

## Scenario Demo Script (5-10 min walkthrough)

1. **Setup and docs**
   - Run `docker compose up --build`
   - Open `/swagger` and show schema + endpoints quickly.
2. **Scenario 1: concurrent updates**
   - Create one issue.
   - Open two tabs (or two Postman requests) with same `version`.
   - Send update A (assignee) and update B (priority) concurrently.
   - Show one succeeds and one gets `409`; retry loser with latest version; final state has both changes.
   - Show `issue_updated` websocket events.
3. **Scenario 2: sprint completion carry-over**
   - Create/start sprint, add a few issues, transition some to done.
   - Complete sprint with `carry_over_issue_ids`.
   - Show `velocity_completed_points`, `incomplete_items`, and `issue_carry_over` activity entries.
   - Show `sprint_updated` websocket event.
4. **Scenario 3: workflow violation**
   - Attempt `To Do -> Done` transition directly.
   - Show `422` and `allowed_transitions`.
5. **Collaboration and notifications**
   - Add comment with `@mention`, then edit/delete comment.
   - Show notifications endpoint (`mention`, `assignment_changed`, `status_changed`).
6. **Search and filters**
   - Run `/api/search` with structured filters.
   - Run `/api/projects/:id/activity` with `event_type`, `actor_id`, and cursor pagination.

## Design Trade-offs

- **Chose consistency over extreme write throughput**: transactional updates and audit logging favor correctness.
- **Chose simple event payloads**: lightweight websocket messages reduce coupling; clients re-fetch details when needed.
- **Chose server-side validation for UUID/date/custom fields**: clearer API errors and fewer DB-level surprises.
- **Deferred auth/authorization**: kept scope focused on workflow/collaboration engine for assignment objectives.
- **Kept replay buffer in Redis list**: easy reconnection support with bounded memory; long-term event storage remains in DB activity log.

## Common Error Shape

Most error responses follow:
```json
{
  "error": "human readable message"
}
```

For invalid workflow transitions:
```json
{
  "error": "invalid transition",
  "allowed_transitions": ["In Progress", "Done"]
}
```
