# WebSocket + REST Demo Steps (Railway)

Base API URL:
`https://projectmgmtsystem-production-shubhamswiggyassesment.up.railway.app`

Base WS URL:
`wss://projectmgmtsystem-production-shubhamswiggyassesment.up.railway.app`

## 1) Import Postman collection

- Import `postman_collection.json`.
- In collection variables, keep:
  - `baseUrl` as deployed Railway URL
  - `baseWsUrl` as deployed Railway WSS URL
  - `projectId=aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa`
  - `userId=11111111-1111-1111-1111-111111111111`

## 2) Open WebSocket client and keep it running

Use Postman WS tab (or any WS client) with:

`wss://projectmgmtsystem-production-shubhamswiggyassesment.up.railway.app/ws/projects/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa?user_id=11111111-1111-1111-1111-111111111111&since=0`

Click Connect and keep this tab open.

## 3) In another tab, run REST calls

Run these requests in order:

1. `Issues -> Create Issue`
   - copy returned `issue_id`
   - set collection variable `issueId`
2. `Issues -> Patch Issue`
3. `Issues -> Transition Issue`
4. `Issues -> Add Comment`

Expected WS events in live tab:
- `issue_created`
- `issue_updated`
- `issue_moved`
- `comment_added`

## 4) Sprint realtime event demo

Run:
1. `Sprints -> Create Sprint` (copy `sprint_id` into variable)
2. `Sprints -> Start Sprint`
3. `Sprints -> Move Issue to Sprint`
4. `Sprints -> Complete Sprint`

Expected WS events include `sprint_updated`.

## 5) Replay missed events (reconnect demo)

- Disconnect WS client.
- Reconnect with `since=<last_event_id_seen>`.
- You should receive events after that ID from replay buffer.

## 6) Quick troubleshooting

- If WS does not connect, ensure URL starts with `wss://` (not `https://`).
- If REST returns project not found, verify `projectId` is seeded ID.
- If issue/sprint IDs are missing, set variables from previous API responses.
