package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"devjam-backend/internal/profile"
)

type ProfileHandler struct {
	store *profile.Store
}

func NewProfileHandler(store *profile.Store) *ProfileHandler {
	return &ProfileHandler{store: store}
}




func userID(c *gin.Context) string {
	if id := c.GetHeader("X-User-Id"); id != "" {
		return id
	}
	return "anonymous"
}

func (h *ProfileHandler) Get(c *gin.Context) {
	p, exists := h.store.Get(userID(c))
	if !exists {
		c.JSON(http.StatusOK, gin.H{"exists": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"exists":          true,
		"impairment_type": p.ImpairmentType,
		"safe_zone":       p.SafeZone,
		"font_scale":      p.FontScale,
		"voice_enabled":   p.VoiceEnabled,
	})
}

type profileRequest struct {
	UserID         string           `json:"user_id"`
	ImpairmentType string           `json:"impairment_type"`
	SafeZone       profile.SafeZone `json:"safe_zone"`
	FontScale      float64          `json:"font_scale"`
	VoiceEnabled   bool             `json:"voice_enabled"`
}

func (h *ProfileHandler) Post(c *gin.Context) {
	var req profileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	uid := userID(c)
	if req.UserID != "" {
		uid = req.UserID
	}
	h.store.Set(uid, profile.Profile{
		ImpairmentType: req.ImpairmentType,
		SafeZone:       req.SafeZone,
		FontScale:      req.FontScale,
		VoiceEnabled:   req.VoiceEnabled,
	})
	c.JSON(http.StatusOK, gin.H{"exists": true})
}
