# Project Demo Script (Architecture-First)

This document is a speaking + execution script for your video walkthrough.

Base URL:
`https://projectmgmtsystem-production-shubhamswiggyassesment.up.railway.app`

WebSocket URL:
`wss://projectmgmtsystem-production-shubhamswiggyassesment.up.railway.app/ws/projects/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa?user_id=11111111-1111-1111-1111-111111111111&since=0`

Seeded IDs:
- `PROJECT_ID=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa`
- `USER_A=11111111-1111-1111-1111-111111111111`
- `USER_B=22222222-2222-2222-2222-222222222222`

---

## 1) Architecture First (What to say)

### 1.1 Why modular monolith (chosen) vs microservices
- **Chosen**: modular monolith in Go (`issues`, `sprints`, `projects`, `search`, `notifications`, `realtime`).
- **Why**: fastest delivery for assignment scope, easier debugging/deployment, clean domain boundaries still preserved.
- **Not microservices now**: higher operational cost (service discovery, tracing, network failures, distributed transactions) without enough payoff at this stage.

### 1.2 Why PostgreSQL (chosen) vs NoSQL-first
- **Chosen**: PostgreSQL as source of truth.
- **Why**: strong relational integrity for project->sprint->issue hierarchy, transitions, constraints, activity trail, and transactional consistency.
- **Not NoSQL-first**: workflow + joins + constraints are central; SQL model fits naturally and safely.

### 1.3 Why Redis + WebSocket (chosen) vs polling-only
- **Chosen**: Redis pub/sub + replay list + WebSocket push.
- **Why**: low-latency realtime fan-out and reconnect replay (`since`), reduced DB polling load.
- **Not polling-only**: higher latency and higher database pressure.

### 1.4 Why optimistic locking (chosen) vs last-write-wins
- **Chosen**: versioned optimistic locking on issues.
- **Why**: prevents silent overwrite in concurrent edits; explicit `409` conflict.
- **Not blind last-write-wins**: can lose user changes silently.

### 1.5 Why cursor pagination (chosen) vs offset pagination
- **Chosen**: cursor style for activity/search.
- **Why**: stable under frequent writes, better scalability for streams.
- **Not offset-only**: unstable page contents when new rows are inserted.

---

## 2) Video Sequence (10-minute exact flow)

## 0:00 - 1:30 Intro + Architecture
Say architecture decisions from section 1.

Show:
- `README.md` architecture section
- `internal/api/router.go`
- `cmd/server/main.go`
- `internal/realtime/hub.go`
- `internal/platform/migrations/sql/001_schema.sql`

## 1:30 - 2:00 Health + docs
```bash
curl -s "https://projectmgmtsystem-production-shubhamswiggyassesment.up.railway.app/healthz"
curl -s "https://projectmgmtsystem-production-shubhamswiggyassesment.up.railway.app/openapi.yaml" | sed -n '1,8p'
```
Also open:
- `/swagger`

## 2:00 - 2:30 Open WebSocket client
Open Postman WebSocket tab and connect:
`wss://projectmgmtsystem-production-shubhamswiggyassesment.up.railway.app/ws/projects/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa?user_id=11111111-1111-1111-1111-111111111111&since=0`

Keep this tab open for the rest of demo.

---

## 3) Functional Demo Calls (exact endpoints)

Set shell variables locally (for easier copy/paste):
```bash
BASE="https://projectmgmtsystem-production-shubhamswiggyassesment.up.railway.app"
PROJECT_ID="aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
USER_A="11111111-1111-1111-1111-111111111111"
USER_B="22222222-2222-2222-2222-222222222222"
```

### 3.1 Create issue
```bash
curl -s -X POST "$BASE/api/projects/$PROJECT_ID/issues" \
  -H "Content-Type: application/json" \
  -d '{
    "type":"story",
    "title":"OAuth login walkthrough issue",
    "description":"Demo item",
    "priority":2,
    "story_points":5,
    "assignee_id":"11111111-1111-1111-1111-111111111111",
    "reporter_id":"22222222-2222-2222-2222-222222222222",
    "labels":["auth","demo"],
    "custom_fields":{}
  }'
```
Copy `issue_id` from response into:
```bash
ISSUE_ID="<paste_issue_id>"
```

Show WS event: `issue_created`.

### 3.2 Board state
```bash
curl -s "$BASE/api/projects/$PROJECT_ID/board"
```

### 3.3 Scenario 1: Concurrency
User A succeeds on version 1:
```bash
curl -s -X PATCH "$BASE/api/issues/$ISSUE_ID" \
  -H "Content-Type: application/json" \
  -d "{
    \"version\":1,
    \"assignee_id\":\"$USER_A\",
    \"actor_id\":\"$USER_A\"
  }"
```
User B stale update, expect `409`:
```bash
curl -s -X PATCH "$BASE/api/issues/$ISSUE_ID" \
  -H "Content-Type: application/json" \
  -d "{
    \"version\":1,
    \"priority\":1,
    \"actor_id\":\"$USER_B\"
  }"
```
Retry with latest version:
```bash
curl -s -X PATCH "$BASE/api/issues/$ISSUE_ID" \
  -H "Content-Type: application/json" \
  -d "{
    \"version\":2,
    \"priority\":1,
    \"actor_id\":\"$USER_B\"
  }"
```

### 3.4 Scenario 3: Workflow violation
Invalid direct transition To Do -> Done:
```bash
curl -s -X POST "$BASE/api/issues/$ISSUE_ID/transitions" \
  -H "Content-Type: application/json" \
  -d "{\"to_status\":\"Done\",\"actor_id\":\"$USER_A\"}"
```
Valid path:
```bash
curl -s -X POST "$BASE/api/issues/$ISSUE_ID/transitions" \
  -H "Content-Type: application/json" \
  -d "{\"to_status\":\"In Progress\",\"actor_id\":\"$USER_A\"}"

curl -s -X POST "$BASE/api/issues/$ISSUE_ID/transitions" \
  -H "Content-Type: application/json" \
  -d "{\"to_status\":\"In Review\",\"actor_id\":\"$USER_A\"}"
```

### 3.5 Comments + mentions + CRUD
Add comment:
```bash
curl -s -X POST "$BASE/api/issues/$ISSUE_ID/comments" \
  -H "Content-Type: application/json" \
  -d "{
    \"user_id\":\"$USER_A\",
    \"content\":\"Please review @bob\",
    \"parent_id\":null
  }"
```
Copy `comment_id`:
```bash
COMMENT_ID="<paste_comment_id>"
```
List comments:
```bash
curl -s "$BASE/api/issues/$ISSUE_ID/comments"
```
Update comment:
```bash
curl -s -X PATCH "$BASE/api/issues/$ISSUE_ID/comments/$COMMENT_ID" \
  -H "Content-Type: application/json" \
  -d "{\"user_id\":\"$USER_A\",\"content\":\"Edited comment\"}"
```
Delete comment:
```bash
curl -s -X DELETE "$BASE/api/issues/$ISSUE_ID/comments/$COMMENT_ID" \
  -H "Content-Type: application/json" \
  -d "{\"user_id\":\"$USER_A\"}"
```

### 3.6 Watch/unwatch
```bash
curl -s -X POST "$BASE/api/issues/$ISSUE_ID/watch?user_id=$USER_A"
curl -s -X DELETE "$BASE/api/issues/$ISSUE_ID/watch?user_id=$USER_A"
```

### 3.7 Scenario 2: Sprint lifecycle + carry-over
Create sprint:
```bash
curl -s -X POST "$BASE/api/projects/$PROJECT_ID/sprints" \
  -H "Content-Type: application/json" \
  -d '{"name":"Demo Sprint","start_date":"2026-04-10","end_date":"2026-04-20"}'
```
Copy `sprint_id`:
```bash
SPRINT_ID="<paste_sprint_id>"
```
Start sprint:
```bash
curl -s -X POST "$BASE/api/sprints/$SPRINT_ID/start"
```
Move issue to sprint:
```bash
curl -s -X POST "$BASE/api/sprints/issues/move" \
  -H "Content-Type: application/json" \
  -d "{\"issue_id\":\"$ISSUE_ID\",\"sprint_id\":\"$SPRINT_ID\"}"
```
Complete sprint (carry-over):
```bash
curl -s -X POST "$BASE/api/sprints/$SPRINT_ID/complete" \
  -H "Content-Type: application/json" \
  -d "{\"new_sprint_id\":\"$SPRINT_ID\",\"carry_over_issue_ids\":[\"$ISSUE_ID\"]}"
```
Show response fields:
- `velocity_completed_points`
- `incomplete_items`

### 3.8 Search and activity filters
Search:
```bash
curl -s "$BASE/api/search?q=OAuth&status=In%20Review&priority_min=1"
```
Activity feed filtered:
```bash
curl -s "$BASE/api/projects/$PROJECT_ID/activity?event_type=issue_updated&actor_id=$USER_A&issue_id=$ISSUE_ID&limit=20"
```

### 3.9 Notifications
```bash
curl -s "$BASE/api/notifications?user_id=$USER_A"
```
Take one notification id:
```bash
NOTIF_ID="<paste_notification_id>"
curl -s -X POST "$BASE/api/notifications/$NOTIF_ID/read"
```

### 3.10 Replay demo
Disconnect WebSocket and reconnect with:
`...&since=<last_seen_event_id>`
Show missed events replaying.

---

## 4) Edge cases to explicitly mention
- Invalid UUID payload returns `400` (pre-DB validation).
- Invalid workflow transition returns `422` + `allowed_transitions`.
- Stale version returns `409` for conflict-safe edits.
- Sprint date format validation (`YYYY-MM-DD`).
- Custom field type and required validation.

---

## 5) Trade-offs + next steps (closing statement)

### Trade-offs made
- Chose modular monolith for speed and maintainability over microservice complexity.
- Realtime replay buffer is bounded (latest ~1000 events).
- Presence currently board/project-level, not issue-level precision.
- Auth/RBAC intentionally minimal for assignment focus.

### Next improvements
- Add auth + RBAC.
- Add issue-level presence tracking.
- Add load tests and latency SLO dashboard.
- Add ERD diagram artifact and architecture ADR docs.
- Extend transition automation hooks (e.g., explicit reviewer assignment strategy).

---

## 6) One-line ending

"This implementation prioritizes correctness, realtime collaboration, and clean extensibility while keeping operational complexity appropriate for an SDE-1 assignment scope."
