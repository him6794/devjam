// Package httpapi exposes the gin handlers for the API contract in
// api.md: POST /api/analyze, GET/POST /api/profile, POST /api/notify_driver.
package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"devjam-backend/internal/skill"
)

type Server struct {
	Analyze  *AnalyzeHandler
	Profile  *ProfileHandler
	Registry *skill.Registry
}

func NewRouter(s *Server) *gin.Engine {
	r := gin.Default()
	alerts := newAlertStore(time.Now)

	r.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })

	r.POST("/api/analyze", s.Analyze.Handle)
	r.GET("/api/profile", s.Profile.Get)
	r.POST("/api/profile", s.Profile.Post)
	r.POST("/api/notify_driver", newNotifyHandler(alerts))
	r.GET("/api/driver_alerts", newAlertsHandler(alerts))

	// Introspection over the skill registry described in plan.md §6 — lets
	// a future LLM tool-calling loop (or a developer) see what's callable.
	r.GET("/api/skills", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"skills": s.Registry.Schemas()})
	})

	return r
}
