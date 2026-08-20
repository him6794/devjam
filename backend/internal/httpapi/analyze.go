package httpapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"devjam-backend/internal/agent"
	"devjam-backend/internal/profile"
	"devjam-backend/internal/skill"
)

type AnalyzeHandler struct {
	orchestrator *agent.Orchestrator
	nearestStops *skill.NearestStops
	stopETA      *skill.StopETA
	visionSign   *skill.VisionReadSign 
	tts          *skill.TTS            
	profiles     *profile.Store
}

func NewAnalyzeHandler(o *agent.Orchestrator, nearest *skill.NearestStops, eta *skill.StopETA, vision *skill.VisionReadSign, tts *skill.TTS, profiles *profile.Store) *AnalyzeHandler {
	return &AnalyzeHandler{orchestrator: o, nearestStops: nearest, stopETA: eta, visionSign: vision, tts: tts, profiles: profiles}
}




const farAwayThresholdM = 150

func (h *AnalyzeHandler) Handle(c *gin.Context) {
	req, err := parseAnalyzeRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Location.Lat == 0 && req.Location.Lng == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "location.lat/location.lng are required"})
		return
	}

	uid := req.UserID
	if uid == "" {
		uid = userID(c)
	}
	imageData, imageMIME := decodeImage(req.Image)
	bb := &agent.Blackboard{Lat: req.Location.Lat, Lon: req.Location.Lng, ImageData: imageData, ImageMIME: imageMIME, WantedRoute: req.WantedRoute}
	ctx := c.Request.Context()

	
	
	
	
	
	
	wave1 := []agent.Agent{
		agent.NewGeoAgent(h.nearestStops),
		agent.NewProfileAgent(h.profiles, uid),
	}
	if h.visionSign != nil && len(imageData) > 0 {
		wave1 = append(wave1, agent.NewVisionAgent(h.visionSign))
	}
	h.orchestrator.RunWave(ctx, bb, wave1...)
	pickStation(bb)

	if !bb.NearestFound || bb.NearestDistM > farAwayThresholdM {
		c.JSON(http.StatusOK, gin.H{
			"status":  "not_found",
			"message": "尚未偵測到站牌",
		})
		return
	}

	
	
	h.orchestrator.RunWave(ctx, bb, agent.NewTransitAgent(h.stopETA))

	sort.SliceStable(bb.Buses, func(i, j int) bool {
		a, b := bb.Buses[i], bb.Buses[j]
		aw, bw := routeMatchesWanted(a.Route, bb.WantedRoute), routeMatchesWanted(b.Route, bb.WantedRoute)
		if aw != bw {
			return aw 
		}
		if a.HasETA != b.HasETA {
			return a.HasETA 
		}
		return a.ETAMinutes < b.ETAMinutes
	})

	buses := make([]gin.H, 0, len(bb.Buses))
	for _, b := range bb.Buses {
		
		
		
		
		
		
		
		
		
		direction := b.Direction
		if direction == "" {
			direction = "方向資訊暫缺"
		}
		etaMinutes := -1
		if b.HasETA {
			etaMinutes = b.ETAMinutes
		}
		buses = append(buses, gin.H{
			"route":       b.Route,
			"urgency":     urgencyFor(b, bb.Profile, bb.WantedRoute),
			"is_wanted":   routeMatchesWanted(b.Route, bb.WantedRoute),
			"direction":   direction,
			"eta_minutes": etaMinutes,
			"has_eta":     b.HasETA,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "success",
		"station_name": bb.NearestName,
		
		
		
		
		"wanted_route":       bb.WantedRoute,
		"wanted_route_found": wantedRouteFound(bb.Buses, bb.WantedRoute),
		"buses":              buses,
		"display": gin.H{
			"safe_zone_position": safeZonePosition(bb.Profile.SafeZone.Y),
			"font_scale":         nonZeroOr(bb.Profile.FontScale, 1.0),
		},
		
		
		
		
		"voice_summary":   voiceSummary(bb.Buses, bb.WantedRoute),
		"voice_audio_url": h.synthesizeVoiceAudioURL(ctx, bb.Buses),
	})
}






func (h *AnalyzeHandler) synthesizeVoiceAudioURL(ctx context.Context, buses []agent.BusReport) string {
	if h.tts == nil {
		return ""
	}
	out, err := h.tts.Do(ctx, skill.TTSIn{Text: voiceSummary(buses, "")})
	if err != nil {
		log.Printf("tts failed: %v", err)
		return ""
	}
	return out.AudioURL
}





func urgency(b agent.BusReport, p agent.ProfileView) string {
	if !b.HasETA {
		return "low"
	}
	buffer := p.ReactionBufferMin
	if buffer <= 0 {
		buffer = 1
	}
	switch {
	case b.ETAMinutes <= buffer:
		return "high"
	case b.ETAMinutes <= buffer+5:
		return "medium"
	default:
		return "low"
	}
}





func urgencyFor(b agent.BusReport, p agent.ProfileView, wanted string) string {
	u := urgency(b, p)
	if wanted != "" && !routeMatchesWanted(b.Route, wanted) {
		return "low"
	}
	return u
}




func routeMatchesWanted(route, wanted string) bool {
	wanted = strings.TrimSpace(wanted)
	if wanted == "" {
		return false
	}
	return strings.TrimSpace(route) == wanted
}



func wantedRouteFound(buses []agent.BusReport, wanted string) bool {
	for _, b := range buses {
		if routeMatchesWanted(b.Route, wanted) {
			return true
		}
	}
	return false
}




func safeZonePosition(y float64) string {
	switch {
	case y <= 0:
		return "top" 
	case y < 33:
		return "top"
	case y < 66:
		return "middle"
	default:
		return "bottom"
	}
}

func nonZeroOr(v, fallback float64) float64 {
	if v <= 0 {
		return fallback
	}
	return v
}

func voiceSummary(buses []agent.BusReport, wanted string) string {
	if len(buses) == 0 {
		return "目前沒有查到路線資訊"
	}
	first := buses[0]
	if routeMatchesWanted(first.Route, wanted) {
		if first.HasETA {
			return fmt.Sprintf("你要搭的%s路還有%d分鐘進站，%s", first.Route, first.ETAMinutes, first.Direction)
		}
		return fmt.Sprintf("你要搭的%s路目前無即時資訊，%s", first.Route, first.Direction)
	}
	if wanted != "" {
		
		
		
		return fmt.Sprintf("此站牌沒有%s路，最近的是%s路，%s", wanted, first.Route, etaPhrase(first))
	}
	if first.HasETA {
		return fmt.Sprintf("%s路還有%d分鐘進站，%s", first.Route, first.ETAMinutes, first.Direction)
	}
	return fmt.Sprintf("%s路目前無即時資訊，%s", first.Route, first.Direction)
}





func etaPhrase(b agent.BusReport) string {
	if b.HasETA {
		return fmt.Sprintf("%d分鐘進站", b.ETAMinutes)
	}
	return "目前無即時資訊"
}
















func pickStation(bb *agent.Blackboard) {
	if len(bb.Candidates) == 0 {
		return
	}
	if bb.VisionFound {
		named := filterByName(bb.Candidates, bb.VisionStopName)
		switch len(named) {
		case 0:
			
			
			
		case 1:
			lockStation(bb, named[0])
			return
		default:
			if c, ok := uniqueByDirection(named, bb.VisionDestinationText); ok {
				lockStation(bb, c)
				return
			}
			lockStation(bb, named[0]) 
			return
		}
	}
	lockStation(bb, bb.Candidates[0])
}

func lockStation(bb *agent.Blackboard, c agent.Candidate) {
	bb.NearestFound = true
	bb.NearestSlID = c.SlID
	bb.NearestName = c.Name
	bb.NearestDistM = c.Distance
}



func filterByName(candidates []agent.Candidate, visionName string) []agent.Candidate {
	visionName = strings.TrimSpace(visionName)
	if visionName == "" {
		return nil
	}
	var out []agent.Candidate
	for _, c := range candidates {
		if stationNameMatches(c.Name, visionName) {
			out = append(out, c)
		}
	}
	return out
}

func uniqueByDirection(candidates []agent.Candidate, visionDestination string) (agent.Candidate, bool) {
	visionDestination = strings.TrimSpace(visionDestination)
	if visionDestination == "" {
		return agent.Candidate{}, false
	}
	var match agent.Candidate
	matches := 0
	for _, c := range candidates {
		for _, label := range c.DirectionLabels {
			if stationNameMatches(label, visionDestination) {
				match = c
				matches++
				break
			}
		}
	}
	return match, matches == 1
}





func stationNameMatches(indexed, vision string) bool {
	indexed, vision = strings.TrimSpace(indexed), strings.TrimSpace(vision)
	if indexed == "" || vision == "" {
		return false
	}
	return strings.Contains(indexed, vision) || strings.Contains(vision, indexed)
}






func decodeImage(s string) ([]byte, string) {
	if s == "" {
		return nil, ""
	}
	mime := ""
	if strings.HasPrefix(s, "data:") {
		if comma := strings.Index(s, ","); comma != -1 {
			header := s[len("data:"):comma]
			if semi := strings.Index(header, ";"); semi != -1 {
				mime = header[:semi]
			} else {
				mime = header
			}
			s = s[comma+1:]
		}
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(s)
		if err != nil {
			return nil, ""
		}
	}
	if mime == "" {
		mime = http.DetectContentType(data)
	}
	return data, mime
}
