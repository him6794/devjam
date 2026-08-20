


package httpapi

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"devjam-backend/internal/skill"
)

type Server struct {
	Analyze    *AnalyzeHandler
	Profile    *ProfileHandler
	VoiceRoute *VoiceRouteHandler
	Navigate   *NavigateHandler
	LiveGuide  *LiveGuideHandler
	Registry   *skill.Registry
}

func NewRouter(s *Server) *gin.Engine {
	r := gin.Default()
	alerts := newAlertStore(time.Now)

	r.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })

	r.POST("/api/analyze", s.Analyze.Handle)
	r.POST("/api/voice_route", s.VoiceRoute.Handle)
	r.POST("/api/navigate", s.Navigate.Handle)
	
	
	
	r.GET("/api/live_guide", s.LiveGuide.Handle)
	r.GET("/api/profile", s.Profile.Get)
	r.POST("/api/profile", s.Profile.Post)
	r.POST("/api/notify_driver", newNotifyHandler(alerts))
	r.GET("/api/driver_alerts", newAlertsHandler(alerts))

	
	
	r.GET("/api/skills", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"skills": s.Registry.Schemas()})
	})

	return r
}
