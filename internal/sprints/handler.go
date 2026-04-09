package sprints

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"project-management-platform/internal/realtime"
)

type Handler struct {
	db  *pgxpool.Pool
	hub *realtime.Hub
}

func NewHandler(db *pgxpool.Pool, hub *realtime.Hub) *Handler {
	return &Handler{db: db, hub: hub}
}

type createSprintRequest struct {
	Name      string `json:"name"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

func (h *Handler) Create(c *gin.Context) {
	projectID := c.Param("id")
	var req createSprintRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" || req.StartDate == "" || req.EndDate == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if _, err := time.Parse("2006-01-02", req.StartDate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date, expected YYYY-MM-DD"})
		return
	}
	if _, err := time.Parse("2006-01-02", req.EndDate); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date, expected YYYY-MM-DD"})
		return
	}
	id := uuid.NewString()
	_, err := h.db.Exec(c, `INSERT INTO sprints(id, project_id, name, start_date, end_date, status) VALUES ($1,$2,$3,$4,$5,'planned')`, id, projectID, req.Name, req.StartDate, req.EndDate)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	_ = h.hub.Publish(c, projectID, "sprint_updated", gin.H{"action": "created", "sprint_id": id, "project_id": projectID})
	c.JSON(http.StatusCreated, gin.H{"sprint_id": id, "status": "planned"})
}

type updateSprintRequest struct {
	Name      *string `json:"name"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
	Status    *string `json:"status"`
}

func (h *Handler) Update(c *gin.Context) {
	id := c.Param("id")
	var req updateSprintRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if req.StartDate != nil {
		if _, err := time.Parse("2006-01-02", *req.StartDate); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start_date, expected YYYY-MM-DD"})
			return
		}
	}
	if req.EndDate != nil {
		if _, err := time.Parse("2006-01-02", *req.EndDate); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid end_date, expected YYYY-MM-DD"})
			return
		}
	}
	var projectID string
	if err := h.db.QueryRow(c, `SELECT project_id FROM sprints WHERE id=$1`, id).Scan(&projectID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "sprint not found"})
		return
	}
	tag, err := h.db.Exec(c, `
		UPDATE sprints
		SET name=COALESCE($1,name),
		    start_date=COALESCE($2,start_date),
		    end_date=COALESCE($3,end_date),
		    status=COALESCE($4,status)
		WHERE id=$5
	`, req.Name, req.StartDate, req.EndDate, req.Status, id)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "sprint not found"})
		return
	}
	_ = h.hub.Publish(c, projectID, "sprint_updated", gin.H{"action": "updated", "sprint_id": id, "project_id": projectID})
	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (h *Handler) Delete(c *gin.Context) {
	id := c.Param("id")
	tx, err := h.db.Begin(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction failed"})
		return
	}
	defer tx.Rollback(c)
	var projectID string
	if err := tx.QueryRow(c, `SELECT project_id FROM sprints WHERE id=$1`, id).Scan(&projectID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "sprint not found"})
		return
	}
	_, _ = tx.Exec(c, `UPDATE issues SET sprint_id=NULL, updated_at=NOW(), version=version+1 WHERE sprint_id=$1`, id)
	tag, err := tx.Exec(c, `DELETE FROM sprints WHERE id=$1`, id)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "sprint not found"})
		return
	}
	if err := tx.Commit(c); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
		return
	}
	_ = h.hub.Publish(c, projectID, "sprint_updated", gin.H{"action": "deleted", "sprint_id": id, "project_id": projectID})
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

type moveIssueRequest struct {
	IssueID   string  `json:"issue_id"`
	SprintID  *string `json:"sprint_id"`
}

func (h *Handler) MoveIssue(c *gin.Context) {
	var req moveIssueRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.IssueID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	tag, err := h.db.Exec(c, `UPDATE issues SET sprint_id=$1, updated_at=NOW(), version=version+1 WHERE id=$2`, req.SprintID, req.IssueID)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "issue not found"})
		return
	}
	var projectID string
	if err := h.db.QueryRow(c, `SELECT project_id FROM issues WHERE id=$1`, req.IssueID).Scan(&projectID); err == nil {
		sprintID := any(nil)
		if req.SprintID != nil {
			sprintID = *req.SprintID
		}
		h.logActivity(c, projectID, req.IssueID, nil, "issue_sprint_moved", gin.H{"to_sprint_id": sprintID})
		_ = h.hub.Publish(c, projectID, "sprint_updated", gin.H{"action": "issue_moved", "issue_id": req.IssueID, "sprint_id": sprintID})
	}
	c.JSON(http.StatusOK, gin.H{"status": "moved"})
}

func (h *Handler) List(c *gin.Context) {
	projectID := c.Param("id")
	rows, err := h.db.Query(c, `
		SELECT id, name, start_date, end_date, status, created_at
		FROM sprints WHERE project_id=$1 ORDER BY created_at DESC
	`, projectID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, name, status string
		var startDate, endDate, createdAt any
		_ = rows.Scan(&id, &name, &startDate, &endDate, &status, &createdAt)
		out = append(out, gin.H{"id": id, "name": name, "start_date": startDate, "end_date": endDate, "status": status, "created_at": createdAt})
	}
	c.JSON(http.StatusOK, gin.H{"sprints": out})
}

func (h *Handler) Start(c *gin.Context) {
	id := c.Param("id")
	var projectID string
	if err := h.db.QueryRow(c, `SELECT project_id FROM sprints WHERE id=$1`, id).Scan(&projectID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "sprint not found"})
		return
	}
	_, err := h.db.Exec(c, `UPDATE sprints SET status='active' WHERE id=$1`, id)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	_ = h.hub.Publish(c, projectID, "sprint_updated", gin.H{"action": "started", "sprint_id": id, "project_id": projectID})
	c.JSON(http.StatusOK, gin.H{"status": "active"})
}

type completeSprintRequest struct {
	NewSprintID  *string  `json:"new_sprint_id"`
	CarryOverIDs []string `json:"carry_over_issue_ids"`
}

func (h *Handler) Complete(c *gin.Context) {
	id := c.Param("id")
	var req completeSprintRequest
	_ = c.ShouldBindJSON(&req)

	tx, err := h.db.Begin(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction failed"})
		return
	}
	defer tx.Rollback(c)
	var projectID string
	if err := tx.QueryRow(c, `SELECT project_id FROM sprints WHERE id=$1`, id).Scan(&projectID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "sprint not found"})
		return
	}

	_, err = tx.Exec(c, `UPDATE sprints SET status='completed' WHERE id=$1`, id)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if req.NewSprintID != nil && len(req.CarryOverIDs) > 0 {
		_, err = tx.Exec(c, `UPDATE issues SET sprint_id=$1, updated_at=NOW(), version=version+1 WHERE id=ANY($2::uuid[])`, *req.NewSprintID, req.CarryOverIDs)
		if err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "carry-over update failed"})
			return
		}
		for _, carried := range req.CarryOverIDs {
			h.logActivityTx(c, tx, projectID, carried, nil, "issue_carry_over", gin.H{"from_sprint_id": id, "to_sprint_id": *req.NewSprintID})
		}
	}

	var completedPoints int
	_ = tx.QueryRow(c, `
		SELECT COALESCE(SUM(i.story_points),0)
		FROM issues i JOIN statuses s ON s.id=i.status_id
		WHERE i.sprint_id=$1 AND s.category='done'
	`, id).Scan(&completedPoints)

	rows, _ := tx.Query(c, `
		SELECT i.id, i.issue_key, i.story_points
		FROM issues i JOIN statuses s ON s.id=i.status_id
		WHERE i.sprint_id=$1 AND s.category <> 'done'
	`, id)
	defer rows.Close()
	incomplete := []gin.H{}
	for rows.Next() {
		var issueID, key string
		var points int
		_ = rows.Scan(&issueID, &key, &points)
		incomplete = append(incomplete, gin.H{"issue_id": issueID, "issue_key": key, "story_points": points})
	}

	if err := tx.Commit(c); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
		return
	}
	_ = h.hub.Publish(c, projectID, "sprint_updated", gin.H{
		"action":                    "completed",
		"sprint_id":                 id,
		"project_id":                projectID,
		"velocity_completed_points": completedPoints,
		"incomplete_count":          len(incomplete),
	})
	c.JSON(http.StatusOK, gin.H{"status": "completed", "velocity_completed_points": completedPoints, "incomplete_items": incomplete})
}

func (h *Handler) logActivity(ctx context.Context, projectID, issueID string, actorID *string, eventType string, payload any) {
	b, _ := json.Marshal(payload)
	_, _ = h.db.Exec(ctx, `
		INSERT INTO activity_log(project_id, issue_id, actor_id, event_type, payload)
		VALUES($1,$2,$3,$4,$5::jsonb)
	`, projectID, issueID, actorID, eventType, string(b))
}

func (h *Handler) logActivityTx(ctx context.Context, tx pgx.Tx, projectID, issueID string, actorID *string, eventType string, payload any) {
	b, _ := json.Marshal(payload)
	_, _ = tx.Exec(ctx, `
		INSERT INTO activity_log(project_id, issue_id, actor_id, event_type, payload)
		VALUES($1,$2,$3,$4,$5::jsonb)
	`, projectID, issueID, actorID, eventType, string(b))
}
