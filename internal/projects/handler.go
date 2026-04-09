package projects

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db *pgxpool.Pool
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}

func (h *Handler) Board(c *gin.Context) {
	projectID := c.Param("id")
	statusRows, err := h.db.Query(c, `SELECT id, name, category, sort_order FROM statuses WHERE project_id=$1 ORDER BY sort_order`, projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load statuses"})
		return
	}
	defer statusRows.Close()
	statuses := []gin.H{}
	for statusRows.Next() {
		var id, name, cat string
		var order int
		_ = statusRows.Scan(&id, &name, &cat, &order)
		issuesRows, _ := h.db.Query(c, `
			SELECT i.id, i.issue_key, i.title, i.priority, i.story_points, i.updated_at
			FROM issues i WHERE i.project_id=$1 AND i.status_id=$2 ORDER BY i.updated_at DESC
		`, projectID, id)
		issues := []gin.H{}
		for issuesRows.Next() {
			var iid, key, title string
			var prio, points int
			var updatedAt any
			_ = issuesRows.Scan(&iid, &key, &title, &prio, &points, &updatedAt)
			issues = append(issues, gin.H{"id": iid, "issue_key": key, "title": title, "priority": prio, "story_points": points, "updated_at": updatedAt})
		}
		issuesRows.Close()
		statuses = append(statuses, gin.H{"id": id, "name": name, "category": cat, "issues": issues})
	}
	c.JSON(http.StatusOK, gin.H{"project_id": projectID, "columns": statuses})
}

func (h *Handler) Activity(c *gin.Context) {
	projectID := c.Param("id")
	limit := 50
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 200 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be between 1 and 200"})
			return
		}
		limit = n
	}
	cursor := c.Query("cursor")
	eventType := strings.TrimSpace(c.Query("event_type"))
	actorID := strings.TrimSpace(c.Query("actor_id"))
	issueID := strings.TrimSpace(c.Query("issue_id"))

	sql := `
		SELECT id, issue_id, actor_id, event_type, payload, created_at
		FROM activity_log
		WHERE project_id=$1
	`
	args := []any{projectID}
	argIdx := 2
	if cursor != "" {
		sql += " AND id < $" + itoa(argIdx)
		args = append(args, cursor)
		argIdx++
	}
	if eventType != "" {
		sql += " AND lower(event_type)=lower($" + itoa(argIdx) + ")"
		args = append(args, eventType)
		argIdx++
	}
	if actorID != "" {
		sql += " AND actor_id=$" + itoa(argIdx)
		args = append(args, actorID)
		argIdx++
	}
	if issueID != "" {
		sql += " AND issue_id=$" + itoa(argIdx)
		args = append(args, issueID)
		argIdx++
	}
	sql += " ORDER BY id DESC LIMIT $" + itoa(argIdx)
	args = append(args, limit)

	rows, err := h.db.Query(c, sql, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "activity query failed"})
		return
	}
	defer rows.Close()
	events := []gin.H{}
	var nextCursor int64
	for rows.Next() {
		var id int64
		var issueID, actorID *string
		var typ string
		var payload []byte
		var createdAt any
		_ = rows.Scan(&id, &issueID, &actorID, &typ, &payload, &createdAt)
		nextCursor = id
		events = append(events, gin.H{
			"id":         id,
			"issue_id":   issueID,
			"actor_id":   actorID,
			"event_type": typ,
			"payload":    jsonRaw(payload),
			"created_at": createdAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"events": events, "next_cursor": nextCursor})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func jsonRaw(b []byte) any {
	if len(b) == 0 {
		return gin.H{}
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return gin.H{}
	}
	return out
}
