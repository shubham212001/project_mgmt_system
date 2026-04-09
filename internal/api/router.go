package api

import (
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	"project-management-platform/internal/issues"
	"project-management-platform/internal/notifications"
	"project-management-platform/internal/docs"
	"project-management-platform/internal/projects"
	"project-management-platform/internal/realtime"
	"project-management-platform/internal/search"
	"project-management-platform/internal/sprints"
)

func NewRouter(db *pgxpool.Pool, hub *realtime.Hub) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger(), cors.Default())

	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.GET("/ws/projects/:projectID", hub.HandleWS)
	docs.RegisterRoutes(r)

	issueH := issues.NewHandler(db, hub)
	projectH := projects.NewHandler(db)
	sprintH := sprints.NewHandler(db, hub)
	searchH := search.NewHandler(db)
	notificationH := notifications.NewHandler(db)

	api := r.Group("/api")
	{
		api.POST("/projects/:id/issues", issueH.Create)
		api.GET("/projects/:id/board", projectH.Board)
		api.PATCH("/issues/:id", issueH.Patch)
		api.POST("/issues/:id/transitions", issueH.Transition)
		api.GET("/projects/:id/sprints", sprintH.List)
		api.POST("/projects/:id/sprints", sprintH.Create)
		api.PATCH("/sprints/:id", sprintH.Update)
		api.DELETE("/sprints/:id", sprintH.Delete)
		api.POST("/sprints/:id/start", sprintH.Start)
		api.POST("/sprints/:id/complete", sprintH.Complete)
		api.POST("/sprints/issues/move", sprintH.MoveIssue)
		api.GET("/issues/:id/comments", issueH.ListComments)
		api.POST("/issues/:id/comments", issueH.AddComment)
		api.PATCH("/issues/:id/comments/:commentID", issueH.UpdateComment)
		api.DELETE("/issues/:id/comments/:commentID", issueH.DeleteComment)
		api.GET("/projects/:id/activity", projectH.Activity)
		api.GET("/search", searchH.Search)
		api.POST("/issues/:id/watch", issueH.Watch)
		api.DELETE("/issues/:id/watch", issueH.Unwatch)
		api.GET("/notifications", notificationH.List)
		api.POST("/notifications/:id/read", notificationH.MarkRead)
	}

	return r
}
