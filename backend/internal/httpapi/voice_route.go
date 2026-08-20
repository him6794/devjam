package httpapi

import (
	"encoding/base64"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"devjam-backend/internal/skill"
)

const maxVoiceAudioBytes = 10 << 20










type VoiceRouteHandler struct {
	stt     *skill.SpeechToText
	extract *skill.RouteExtract
}

func NewVoiceRouteHandler(stt *skill.SpeechToText, extract *skill.RouteExtract) *VoiceRouteHandler {
	return &VoiceRouteHandler{stt: stt, extract: extract}
}

type voiceRouteRequest struct {
	Text        string `json:"text"`
	AudioBase64 string `json:"audio_base64"`
	AudioMIME   string `json:"audio_mime"`
}

func (h *VoiceRouteHandler) Handle(c *gin.Context) {
	var req voiceRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "voice_route: parse JSON: " + err.Error()})
		return
	}
	ctx := c.Request.Context()

	transcript := strings.TrimSpace(req.Text)
	if transcript == "" && req.AudioBase64 != "" {
		if h.stt == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":  "stt_unavailable",
				"message": "語音辨識服務尚未設定，請改用文字輸入路線號碼。",
			})
			return
		}
		audio, err := decodeAudioBase64(req.AudioBase64)
		if err != nil || len(audio) > maxVoiceAudioBytes {
			c.JSON(http.StatusBadRequest, gin.H{"error": "voice_route: audio_base64 must be valid base64 audio"})
			return
		}
		out, err := h.stt.Do(ctx, skill.STTIn{Audio: audio, MIMEType: req.AudioMIME})
		if err != nil {
			log.Printf("voice_route: stt failed: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status":  "stt_failed",
				"message": "聽不清楚，請再說一次，或改用文字輸入路線號碼。",
			})
			return
		}
		transcript = strings.TrimSpace(out.Transcript)
	}
	if transcript == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "voice_route: text or audio_base64 is required"})
		return
	}

	out, err := h.extract.Do(ctx, skill.RouteExtractIn{Text: transcript})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "voice_route: route extraction failed"})
		return
	}
	route := normalizeRouteCode(out.Route)
	if !out.Found || route == "" {
		c.JSON(http.StatusOK, gin.H{
			"status":  "no_route",
			"message": "沒有聽到路線號碼，請說出例如「307」。",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":     "success",
		"route":      route,
		"transcript": transcript,
	})
}



func normalizeRouteCode(route string) string {
	route = strings.TrimSpace(route)
	for _, suffix := range []string{"路", "號", "公車"} {
		route = strings.TrimSuffix(route, suffix)
	}
	return strings.TrimSpace(route)
}



func decodeAudioBase64(s string) ([]byte, error) {
	if strings.HasPrefix(s, "data:") {
		if comma := strings.Index(s, ","); comma != -1 {
			s = s[comma+1:]
		}
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return base64.RawStdEncoding.DecodeString(s)
	}
	return data, nil
}
