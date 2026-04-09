package notifications

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db *pgxpool.Pool
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
}

func (h *Handler) List(c *gin.Context) {
	userID := c.Query("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}
	rows, err := h.db.Query(c, `
		SELECT id, project_id, issue_id, type, payload, is_read, created_at
		FROM notifications
		WHERE user_id=$1
		ORDER BY created_at DESC
		LIMIT 100
	`, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	var out []gin.H
	for rows.Next() {
		var id, typ string
		var projectID, issueID *string
		var payload []byte
		var isRead bool
		var createdAt any
		_ = rows.Scan(&id, &projectID, &issueID, &typ, &payload, &isRead, &createdAt)
		out = append(out, gin.H{
			"id":         id,
			"project_id": projectID,
			"issue_id":   issueID,
			"type":       typ,
			"payload":    toJSON(payload),
			"is_read":    isRead,
			"created_at": createdAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"notifications": out})
}

func (h *Handler) MarkRead(c *gin.Context) {
	id := c.Param("id")
	tag, err := h.db.Exec(c, `UPDATE notifications SET is_read=TRUE WHERE id=$1`, id)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "notification not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "read"})
}

func toJSON(b []byte) any {
	var out any
	if len(b) == 0 {
		return gin.H{}
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return gin.H{}
	}
	return out
}
