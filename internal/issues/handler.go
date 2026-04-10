package issues

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
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

type createIssueRequest struct {
	Type        string                 `json:"type"`
	Title       string                 `json:"title"`
	Description string                 `json:"description"`
	Priority    int                    `json:"priority"`
	StoryPoints int                    `json:"story_points"`
	AssigneeID  *string                `json:"assignee_id"`
	ReporterID  *string                `json:"reporter_id"`
	ParentID    *string                `json:"parent_id"`
	SprintID    *string                `json:"sprint_id"`
	Labels      []string               `json:"labels"`
	Custom      map[string]interface{} `json:"custom_fields"`
}

func (h *Handler) Create(c *gin.Context) {
	projectID := c.Param("id")
	var req createIssueRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Title == "" || req.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	if err := validateOptionalUUID("assignee_id", req.AssigneeID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateOptionalUUID("reporter_id", req.ReporterID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateOptionalUUID("parent_id", req.ParentID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateOptionalUUID("sprint_id", req.SprintID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	issueID := uuid.NewString()
	var key string
	if err := h.db.QueryRow(c, `SELECT key FROM projects WHERE id=$1`, projectID).Scan(&key); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "project not found"})
		return
	}
	var next int
	_ = h.db.QueryRow(c, `SELECT COALESCE(MAX(CAST(split_part(issue_key, '-', 2) AS INT)),0)+1 FROM issues WHERE project_id=$1`, projectID).Scan(&next)
	issueKey := key + "-" + itoa(next)
	var defaultStatusID string
	if err := h.db.QueryRow(c, `SELECT id FROM statuses WHERE project_id=$1 ORDER BY sort_order ASC LIMIT 1`, projectID).Scan(&defaultStatusID); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "project has no statuses"})
		return
	}
	customJSON, _ := json.Marshal(req.Custom)
	if err := h.validateCustomFields(c, projectID, req.Custom, false); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	_, err := h.db.Exec(c, `
		INSERT INTO issues(id, issue_key, project_id, sprint_id, parent_id, type, title, description, status_id, priority, story_points, assignee_id, reporter_id, labels, custom_fields)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb)
	`, issueID, issueKey, projectID, req.SprintID, req.ParentID, strings.ToLower(req.Type), req.Title, req.Description, defaultStatusID, req.Priority, req.StoryPoints, req.AssigneeID, req.ReporterID, req.Labels, string(customJSON))
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if req.AssigneeID != nil && *req.AssigneeID != "" {
		h.createNotification(c, *req.AssigneeID, projectID, &issueID, "assignment_changed", gin.H{
			"issue_id":  issueID,
			"issue_key": issueKey,
			"reason":    "assigned_on_create",
		})
	}
	h.logActivity(c, projectID, issueID, req.ReporterID, "issue_created", gin.H{"title": req.Title})
	_ = h.hub.Publish(c, projectID, "issue_created", gin.H{"issue_id": issueID, "issue_key": issueKey})
	c.JSON(http.StatusCreated, gin.H{"issue_id": issueID, "issue_key": issueKey})
}

type patchIssueRequest struct {
	Title       *string                `json:"title"`
	Description *string                `json:"description"`
	Priority    *int                   `json:"priority"`
	AssigneeID  *string                `json:"assignee_id"`
	StoryPoints *int                   `json:"story_points"`
	Labels      *[]string              `json:"labels"`
	SprintID    *string                `json:"sprint_id"`
	Custom      *map[string]interface{} `json:"custom_fields"`
	Version     int                    `json:"version"`
	ActorID     *string                `json:"actor_id"`
}

func (h *Handler) Patch(c *gin.Context) {
	issueID := c.Param("id")
	var req patchIssueRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Version <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "version is required"})
		return
	}
	if err := validateOptionalUUID("assignee_id", req.AssigneeID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateOptionalUUID("sprint_id", req.SprintID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateOptionalUUID("actor_id", req.ActorID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	tx, err := h.db.Begin(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction failed"})
		return
	}
	defer tx.Rollback(c)

	var projectID string
	var oldAssigneeID *string
	if err := tx.QueryRow(c, `SELECT project_id, assignee_id FROM issues WHERE id=$1`, issueID).Scan(&projectID, &oldAssigneeID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "issue not found"})
		return
	}

	custom := "{}"
	if req.Custom != nil {
		if err := h.validateCustomFields(c, projectID, *req.Custom, true); err != nil {
			c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
			return
		}
		b, _ := json.Marshal(req.Custom)
		custom = string(b)
	}
	tag, err := tx.Exec(c, `
		UPDATE issues
		SET title = COALESCE($1,title),
		    description = COALESCE($2,description),
		    priority = COALESCE($3,priority),
		    assignee_id = COALESCE($4,assignee_id),
		    story_points = COALESCE($5,story_points),
		    labels = COALESCE($6,labels),
		    sprint_id = COALESCE($7,sprint_id),
		    custom_fields = CASE WHEN $8='{}' THEN custom_fields ELSE $8::jsonb END,
		    version = version + 1,
		    updated_at = NOW()
		WHERE id=$9 AND version=$10
	`, req.Title, req.Description, req.Priority, req.AssigneeID, req.StoryPoints, req.Labels, req.SprintID, custom, issueID, req.Version)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "conflict detected, refresh and retry"})
		return
	}
	var newAssigneeID *string
	_ = tx.QueryRow(c, `SELECT assignee_id FROM issues WHERE id=$1`, issueID).Scan(&newAssigneeID)
	if newAssigneeID != nil && *newAssigneeID != "" {
		old := ""
		if oldAssigneeID != nil {
			old = *oldAssigneeID
		}
		if old != *newAssigneeID {
			h.createNotificationTx(c, tx, *newAssigneeID, projectID, &issueID, "assignment_changed", gin.H{
				"issue_id": issueID,
				"reason":   "assignee_updated",
			})
		}
	}
	h.logActivityTx(c, tx, projectID, issueID, req.ActorID, "issue_updated", gin.H{"version": req.Version + 1})
	if err := tx.Commit(c); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
		return
	}
	_ = h.hub.Publish(c, projectID, "issue_updated", gin.H{"issue_id": issueID})
	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

type transitionRequest struct {
	ToStatus string  `json:"to_status"`
	ActorID  *string `json:"actor_id"`
}

func (h *Handler) Transition(c *gin.Context) {
	issueID := c.Param("id")
	var req transitionRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.ToStatus == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "to_status required"})
		return
	}
	tx, err := h.db.Begin(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "transaction failed"})
		return
	}
	defer tx.Rollback(c)

	var projectID, currentStatusID, currentStatus, toStatusID string
	if err := tx.QueryRow(c, `
		SELECT i.project_id, i.status_id, s.name
		FROM issues i JOIN statuses s ON s.id=i.status_id
		WHERE i.id=$1`, issueID).Scan(&projectID, &currentStatusID, &currentStatus); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "issue not found"})
		return
	}
	if err := tx.QueryRow(c, `SELECT id FROM statuses WHERE project_id=$1 AND lower(name)=lower($2)`, projectID, req.ToStatus).Scan(&toStatusID); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "target status not found"})
		return
	}
	var allowed bool
	_ = tx.QueryRow(c, `SELECT EXISTS(SELECT 1 FROM workflow_transitions WHERE project_id=$1 AND from_status_id=$2 AND to_status_id=$3)`, projectID, currentStatusID, toStatusID).Scan(&allowed)
	if !allowed {
		rows, _ := tx.Query(c, `
			SELECT s2.name
			FROM workflow_transitions wt
			JOIN statuses s2 ON s2.id=wt.to_status_id
			WHERE wt.project_id=$1 AND wt.from_status_id=$2
		`, projectID, currentStatusID)
		defer rows.Close()
		var allowedTo []string
		for rows.Next() {
			var name string
			_ = rows.Scan(&name)
			allowedTo = append(allowedTo, name)
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid transition", "allowed_transitions": allowedTo})
		return
	}
	if err := h.validateTransitionConditions(c, tx, issueID, req.ToStatus); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}

	_, err = tx.Exec(c, `UPDATE issues SET status_id=$1, updated_at=NOW(), version=version+1 WHERE id=$2`, toStatusID, issueID)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	var assigneeID *string
	_ = tx.QueryRow(c, `SELECT assignee_id FROM issues WHERE id=$1`, issueID).Scan(&assigneeID)
	h.logActivityTx(c, tx, projectID, issueID, req.ActorID, "issue_transitioned", gin.H{"from": currentStatus, "to": req.ToStatus})
	if assigneeID != nil && *assigneeID != "" {
		h.createNotificationTx(c, tx, *assigneeID, projectID, &issueID, "status_changed", gin.H{
			"issue_id": issueID,
			"from":     currentStatus,
			"to":       req.ToStatus,
		})
	}
	if strings.EqualFold(req.ToStatus, "In Review") {
		_, _ = tx.Exec(c, `
			INSERT INTO notifications(user_id, project_id, issue_id, type, payload)
			SELECT assignee_id, project_id, id, 'review_needed', '{"reason":"moved_to_in_review"}'::jsonb
			FROM issues WHERE id=$1 AND assignee_id IS NOT NULL
		`, issueID)
	}
	if err := tx.Commit(c); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "commit failed"})
		return
	}
	_ = h.hub.Publish(c, projectID, "issue_moved", gin.H{"issue_id": issueID, "to_status": req.ToStatus})
	c.JSON(http.StatusOK, gin.H{"status": "transitioned"})
}

func (h *Handler) validateTransitionConditions(ctx context.Context, tx pgx.Tx, issueID, toStatus string) error {
	if strings.EqualFold(toStatus, "Done") {
		var assigneeID *string
		var storyPoints int
		if err := tx.QueryRow(ctx, `SELECT assignee_id, story_points FROM issues WHERE id=$1`, issueID).Scan(&assigneeID, &storyPoints); err != nil {
			return err
		}
		if assigneeID == nil || *assigneeID == "" {
			return fmt.Errorf("cannot move to Done: assignee is required")
		}
		if storyPoints <= 0 {
			return fmt.Errorf("cannot move to Done: story_points must be > 0")
		}
	}
	return nil
}

func (h *Handler) ListComments(c *gin.Context) {
	issueID := c.Param("id")
	rows, err := h.db.Query(c, `
		SELECT c.id, c.parent_id, c.content, c.created_at, u.id, u.display_name
		FROM comments c
		JOIN users u ON u.id=c.user_id
		WHERE c.issue_id=$1
		ORDER BY c.created_at ASC
	`, issueID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query failed"})
		return
	}
	defer rows.Close()
	var out []gin.H
	for rows.Next() {
		var id string
		var parentID *string
		var content, userID, userName string
		var createdAt time.Time
		_ = rows.Scan(&id, &parentID, &content, &createdAt, &userID, &userName)
		out = append(out, gin.H{
			"id":         id,
			"parent_id":  parentID,
			"content":    content,
			"created_at": createdAt,
			"user":       gin.H{"user_id": userID, "display_name": userName},
		})
	}
	c.JSON(http.StatusOK, gin.H{"comments": out})
}

type addCommentRequest struct {
	UserID   string  `json:"user_id"`
	Content  string  `json:"content"`
	ParentID *string `json:"parent_id"`
}

func (h *Handler) AddComment(c *gin.Context) {
	issueID := c.Param("id")
	var req addCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.UserID == "" || strings.TrimSpace(req.Content) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	commentID := uuid.NewString()
	var projectID string
	if err := h.db.QueryRow(c, `SELECT project_id FROM issues WHERE id=$1`, issueID).Scan(&projectID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "issue not found"})
		return
	}
	if _, err := h.db.Exec(c, `INSERT INTO comments(id, issue_id, parent_id, user_id, content) VALUES ($1,$2,$3,$4,$5)`, commentID, issueID, req.ParentID, req.UserID, req.Content); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	h.logActivity(c, projectID, issueID, &req.UserID, "comment_added", gin.H{"comment_id": commentID})
	if err := h.createMentionNotifications(c, projectID, issueID, req.Content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "mention notification failed"})
		return
	}
	if err := h.createCommentNotifications(c, projectID, issueID, req.UserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "comment notification failed"})
		return
	}
	_ = h.hub.Publish(c, projectID, "comment_added", gin.H{"issue_id": issueID, "comment_id": commentID})
	c.JSON(http.StatusCreated, gin.H{"comment_id": commentID})
}

type updateCommentRequest struct {
	UserID  string `json:"user_id"`
	Content string `json:"content"`
}

func (h *Handler) UpdateComment(c *gin.Context) {
	issueID := c.Param("id")
	commentID := c.Param("commentID")
	var req updateCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.UserID == "" || strings.TrimSpace(req.Content) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	tag, err := h.db.Exec(c, `UPDATE comments SET content=$1 WHERE id=$2 AND issue_id=$3 AND user_id=$4`, req.Content, commentID, issueID, req.UserID)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "comment not found"})
		return
	}
	var projectID string
	_ = h.db.QueryRow(c, `SELECT project_id FROM issues WHERE id=$1`, issueID).Scan(&projectID)
	h.logActivity(c, projectID, issueID, &req.UserID, "comment_updated", gin.H{"comment_id": commentID})
	if err := h.createMentionNotifications(c, projectID, issueID, req.Content); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "mention notification failed"})
		return
	}
	if err := h.createCommentNotifications(c, projectID, issueID, req.UserID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "comment notification failed"})
		return
	}
	_ = h.hub.Publish(c, projectID, "comment_updated", gin.H{"issue_id": issueID, "comment_id": commentID})
	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

type deleteCommentRequest struct {
	UserID string `json:"user_id"`
}

func (h *Handler) DeleteComment(c *gin.Context) {
	issueID := c.Param("id")
	commentID := c.Param("commentID")
	var req deleteCommentRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.UserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	tag, err := h.db.Exec(c, `DELETE FROM comments WHERE id=$1 AND issue_id=$2 AND user_id=$3`, commentID, issueID, req.UserID)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if tag.RowsAffected() == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "comment not found"})
		return
	}
	var projectID string
	_ = h.db.QueryRow(c, `SELECT project_id FROM issues WHERE id=$1`, issueID).Scan(&projectID)
	h.logActivity(c, projectID, issueID, &req.UserID, "comment_deleted", gin.H{"comment_id": commentID})
	_ = h.hub.Publish(c, projectID, "comment_deleted", gin.H{"issue_id": issueID, "comment_id": commentID})
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (h *Handler) Watch(c *gin.Context) {
	issueID := c.Param("id")
	userID := c.Query("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}
	_, err := h.db.Exec(c, `INSERT INTO issue_watchers(issue_id, user_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, issueID, userID)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	var projectID string
	if err := h.db.QueryRow(c, `SELECT project_id FROM issues WHERE id=$1`, issueID).Scan(&projectID); err == nil {
		h.logActivity(c, projectID, issueID, &userID, "issue_watched", gin.H{"watcher_id": userID})
	}
	c.JSON(http.StatusOK, gin.H{"status": "watching"})
}

func (h *Handler) Unwatch(c *gin.Context) {
	issueID := c.Param("id")
	userID := c.Query("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}
	_, _ = h.db.Exec(c, `DELETE FROM issue_watchers WHERE issue_id=$1 AND user_id=$2`, issueID, userID)
	var projectID string
	if err := h.db.QueryRow(c, `SELECT project_id FROM issues WHERE id=$1`, issueID).Scan(&projectID); err == nil {
		h.logActivity(c, projectID, issueID, &userID, "issue_unwatched", gin.H{"watcher_id": userID})
	}
	c.JSON(http.StatusOK, gin.H{"status": "unwatched"})
}

func (h *Handler) createMentionNotifications(ctx context.Context, projectID, issueID, text string) error {
	mentions := parseMentions(text)
	if len(mentions) == 0 {
		return nil
	}
	for _, m := range mentions {
		if _, err := h.db.Exec(ctx, `
			INSERT INTO notifications(user_id, project_id, issue_id, type, payload)
			SELECT id, $1, $2, 'mention', jsonb_build_object('mention', $3)
			FROM users WHERE lower(split_part(email,'@',1))=lower($3)
		`, projectID, issueID, m); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) createCommentNotifications(ctx context.Context, projectID, issueID, authorID string) error {
	recipients := map[string]struct{}{}
	var assigneeID *string
	if err := h.db.QueryRow(ctx, `SELECT assignee_id FROM issues WHERE id=$1`, issueID).Scan(&assigneeID); err == nil {
		if assigneeID != nil && *assigneeID != "" && *assigneeID != authorID {
			recipients[*assigneeID] = struct{}{}
		}
	}
	rows, err := h.db.Query(ctx, `SELECT user_id FROM issue_watchers WHERE issue_id=$1`, issueID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return err
		}
		if uid != "" && uid != authorID {
			recipients[uid] = struct{}{}
		}
	}
	if len(recipients) == 0 {
		return nil
	}
	for uid := range recipients {
		if _, err := h.db.Exec(ctx, `
			INSERT INTO notifications(user_id, project_id, issue_id, type, payload)
			VALUES($1,$2,$3,'comment_added',jsonb_build_object('issue_id',$3,'reason','comment_activity'))
		`, uid, projectID, issueID); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) createNotification(ctx context.Context, userID, projectID string, issueID *string, typ string, payload any) {
	b, _ := json.Marshal(payload)
	_, _ = h.db.Exec(ctx, `
		INSERT INTO notifications(user_id, project_id, issue_id, type, payload)
		VALUES($1,$2,$3,$4,$5::jsonb)
	`, userID, projectID, issueID, typ, string(b))
}

func (h *Handler) createNotificationTx(ctx context.Context, tx pgx.Tx, userID, projectID string, issueID *string, typ string, payload any) {
	b, _ := json.Marshal(payload)
	_, _ = tx.Exec(ctx, `
		INSERT INTO notifications(user_id, project_id, issue_id, type, payload)
		VALUES($1,$2,$3,$4,$5::jsonb)
	`, userID, projectID, issueID, typ, string(b))
}

var mentionRe = regexp.MustCompile(`@([a-zA-Z0-9_\-\.]+)`)

func parseMentions(text string) []string {
	ms := mentionRe.FindAllStringSubmatch(text, -1)
	out := make([]string, 0, len(ms))
	seen := map[string]struct{}{}
	for _, m := range ms {
		if len(m) < 2 {
			continue
		}
		k := strings.ToLower(m[1])
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, m[1])
	}
	return out
}

func (h *Handler) validateCustomFields(ctx context.Context, projectID string, input map[string]interface{}, partial bool) error {
	rows, err := h.db.Query(ctx, `
		SELECT field_key, field_type, options, required
		FROM custom_field_definitions
		WHERE project_id=$1
	`, projectID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type def struct {
		typ      string
		options  []string
		required bool
	}
	defs := map[string]def{}
	for rows.Next() {
		var k, t string
		var optsRaw []byte
		var req bool
		_ = rows.Scan(&k, &t, &optsRaw, &req)
		d := def{typ: t, required: req}
		var opts []string
		_ = json.Unmarshal(optsRaw, &opts)
		d.options = opts
		defs[k] = d
	}
	if len(defs) == 0 {
		return nil
	}

	if !partial {
		for k, d := range defs {
			if d.required {
				if _, ok := input[k]; !ok {
					return fmt.Errorf("required custom field missing: %s", k)
				}
			}
		}
	}

	for k, v := range input {
		d, ok := defs[k]
		if !ok {
			return fmt.Errorf("unknown custom field: %s", k)
		}
		switch d.typ {
		case "text":
			if _, ok := v.(string); !ok {
				return fmt.Errorf("custom field %s must be text", k)
			}
		case "number":
			switch v.(type) {
			case float64, int, int32, int64:
			default:
				return fmt.Errorf("custom field %s must be number", k)
			}
		case "date":
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("custom field %s must be date string", k)
			}
			if _, err := time.Parse("2006-01-02", s); err != nil {
				return fmt.Errorf("custom field %s must be YYYY-MM-DD", k)
			}
		case "dropdown":
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("custom field %s must be dropdown option string", k)
			}
			if len(d.options) > 0 {
				found := false
				for _, o := range d.options {
					if strings.EqualFold(o, s) {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("custom field %s invalid option", k)
				}
			}
		}
	}
	return nil
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

func validateOptionalUUID(field string, v *string) error {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	if _, err := uuid.Parse(*v); err != nil {
		return fmt.Errorf("%s must be a valid UUID", field)
	}
	return nil
}

