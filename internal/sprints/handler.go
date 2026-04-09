package sprints

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	db *pgxpool.Pool
}

func NewHandler(db *pgxpool.Pool) *Handler {
	return &Handler{db: db}
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
	_, _ = tx.Exec(c, `UPDATE issues SET sprint_id=NULL WHERE sprint_id=$1`, id)
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
	_, err := h.db.Exec(c, `UPDATE sprints SET status='active' WHERE id=$1`, id)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
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

	_, err = tx.Exec(c, `UPDATE sprints SET status='completed' WHERE id=$1`, id)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if req.NewSprintID != nil && len(req.CarryOverIDs) > 0 {
		_, err = tx.Exec(c, `UPDATE issues SET sprint_id=$1 WHERE id=ANY($2::uuid[])`, *req.NewSprintID, req.CarryOverIDs)
		if err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "carry-over update failed"})
			return
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
	c.JSON(http.StatusOK, gin.H{"status": "completed", "velocity_completed_points": completedPoints, "incomplete_items": incomplete})
}
