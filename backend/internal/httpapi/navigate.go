package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"devjam-backend/internal/skill"
)




type NavigateHandler struct {
	navigate *skill.Navigate
}

func NewNavigateHandler(navigate *skill.Navigate) *NavigateHandler {
	return &NavigateHandler{navigate: navigate}
}

type navigateRequest struct {
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	Destination string  `json:"destination"`
}

func (h *NavigateHandler) Handle(c *gin.Context) {
	var req navigateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "navigate: parse JSON: " + err.Error()})
		return
	}
	if req.Lat == 0 && req.Lng == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "lat/lng are required"})
		return
	}
	if req.Destination == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "destination is required"})
		return
	}

	out, err := h.navigate.Do(c.Request.Context(), skill.NavigateIn{
		OriginLat:   req.Lat,
		OriginLon:   req.Lng,
		Destination: req.Destination,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}
