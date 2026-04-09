package search

import (
	"net/http"
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

func (h *Handler) Search(c *gin.Context) {
	q := strings.TrimSpace(c.Query("q"))
	if q == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q is required"})
		return
	}
	status := c.Query("status")
	assignee := c.Query("assignee")
	priorityMin := c.DefaultQuery("priority_min", "0")
	cursor := c.Query("cursor")
	limit := 25

	sql := `
		SELECT DISTINCT i.id, i.issue_key, i.title, i.description, i.priority, i.updated_at
		FROM issues i
		LEFT JOIN users u ON u.id=i.assignee_id
		LEFT JOIN comments c ON c.issue_id=i.id
		JOIN statuses s ON s.id=i.status_id
		WHERE (
			i.search_vector @@ websearch_to_tsquery('english', $1)
			OR to_tsvector('english', COALESCE(c.content,'')) @@ websearch_to_tsquery('english', $1)
		)
		  AND i.priority >= $2::int
	`
	args := []any{q, priorityMin}
	argIdx := 3
	if status != "" {
		sql += " AND lower(s.name)=lower($" + itoa(argIdx) + ")"
		args = append(args, status)
		argIdx++
	}
	if assignee != "" {
		sql += " AND lower(u.display_name)=lower($" + itoa(argIdx) + ")"
		args = append(args, assignee)
		argIdx++
	}
	if cursor != "" {
		sql += " AND i.updated_at < $" + itoa(argIdx) + "::timestamptz"
		args = append(args, cursor)
		argIdx++
	}
	sql += " ORDER BY i.updated_at DESC LIMIT $" + itoa(argIdx)
	args = append(args, limit)

	rows, err := h.db.Query(c, sql, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "search query failed"})
		return
	}
	defer rows.Close()

	var results []gin.H
	var nextCursor any
	for rows.Next() {
		var id, key, title, desc string
		var priority int
		var updatedAt any
		_ = rows.Scan(&id, &key, &title, &desc, &priority, &updatedAt)
		nextCursor = updatedAt
		results = append(results, gin.H{
			"issue_id":     id,
			"issue_key":    key,
			"title":        title,
			"description":  desc,
			"priority":     priority,
			"updated_at":   updatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"results": results, "next_cursor": nextCursor})
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
