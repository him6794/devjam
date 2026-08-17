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
	visionSign   *skill.VisionReadSign // nil when Gemini credentials aren't configured; analyze falls back to GPS-only
	tts          *skill.TTS            // nil when TTS/Storage credentials aren't configured; voice_summary falls back to ""
	profiles     *profile.Store
}

func NewAnalyzeHandler(o *agent.Orchestrator, nearest *skill.NearestStops, eta *skill.StopETA, vision *skill.VisionReadSign, tts *skill.TTS, profiles *profile.Store) *AnalyzeHandler {
	return &AnalyzeHandler{orchestrator: o, nearestStops: nearest, stopETA: eta, visionSign: vision, tts: tts, profiles: profiles}
}

// farAwayThresholdM matches plan.md §4.1's distance gate: typical urban GPS
// error is 10-30m, so 150m comfortably covers drift without matching the
// user to a station block away.
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
	bb := &agent.Blackboard{Lat: req.Location.Lat, Lon: req.Location.Lng, ImageData: imageData, ImageMIME: imageMIME}
	ctx := c.Request.Context()

	// Wave 1: geo lookup, profile lookup, and (if a photo was submitted
	// and Gemini is configured) reading the sign are fully independent of
	// each other, so they run concurrently — this is the "multiple
	// agents working in parallel" requirement from plan.md §3, not a
	// sequential fallback. Vision's output only feeds pickStation below;
	// it never talks to Geo or vice versa.
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

	// Wave 2: Transit depends on the station Wave 1 picked, so it cannot
	// start earlier.
	h.orchestrator.RunWave(ctx, bb, agent.NewTransitAgent(h.stopETA))

	sort.SliceStable(bb.Buses, func(i, j int) bool {
		a, b := bb.Buses[i], bb.Buses[j]
		if a.HasETA != b.HasETA {
			return a.HasETA // buses with a live ETA sort before "no data"
		}
		return a.ETAMinutes < b.ETAMinutes
	})

	buses := make([]gin.H, 0, len(bb.Buses))
	for _, b := range bb.Buses {
		entry := gin.H{
			"route":   b.Route,
			"urgency": urgency(b, bb.Profile),
		}
		// direction/eta_minutes are always present as keys, null when
		// pda5284 (or this project's 6-route demo index, see plan.md §2.1)
		// has no data for that row — a caller can then tell "no data" (null)
		// apart from "field doesn't exist" without extra guards.
		if b.Direction != "" {
			entry["direction"] = b.Direction
		} else {
			entry["direction"] = nil
		}
		if b.HasETA {
			entry["eta_minutes"] = b.ETAMinutes
		} else {
			entry["eta_minutes"] = nil
		}
		buses = append(buses, entry)
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "success",
		"station_name": bb.NearestName,
		"buses":        buses,
		"display": gin.H{
			"safe_zone_position": safeZonePosition(bb.Profile.SafeZone.Y),
			"font_scale":         nonZeroOr(bb.Profile.FontScale, 1.0),
		},
		// voice_summary is text so the frontend can read it with the rider's
		// own already-configured device screen-reader rate/voice (plan.md §9)
		// instead of a fixed-rate server clip; voice_audio_url is an optional
		// Cloud TTS fallback for browsers without speechSynthesis support.
		"voice_summary":   voiceSummary(bb.Buses),
		"voice_audio_url": h.synthesizeVoiceAudioURL(ctx, bb.Buses),
	})
}

// synthesizeVoiceAudioURL turns the bus list into one spoken sentence and
// returns a public URL to its MP3. Returns "" — never an error — when TTS
// isn't configured or the synthesis call fails, so a Cloud TTS hiccup
// degrades to "no audio fallback" rather than losing the bus data the rest
// of the response already has.
func (h *AnalyzeHandler) synthesizeVoiceAudioURL(ctx context.Context, buses []agent.BusReport) string {
	if h.tts == nil {
		return ""
	}
	out, err := h.tts.Do(ctx, skill.TTSIn{Text: voiceSummary(buses)})
	if err != nil {
		log.Printf("tts failed: %v", err)
		return ""
	}
	return out.AudioURL
}

// urgency folds a route's ETA against the caller's reaction-time buffer
// (profile.Profile.ReactionBufferMinutes): the same 3-minute ETA is "high"
// urgency for a rider who needs longer to reach the curb, but "medium" for
// a rider with no stated impairment.
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

// safeZonePosition maps the profile's safe-zone Y coordinate (0-100, top to
// bottom of frame) to the coarse position keyword the frontend's display
// block expects.
func safeZonePosition(y float64) string {
	switch {
	case y <= 0:
		return "top" // no safe_zone on file
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

func voiceSummary(buses []agent.BusReport) string {
	if len(buses) == 0 {
		return "目前沒有查到路線資訊"
	}
	first := buses[0]
	if first.HasETA {
		return fmt.Sprintf("%s路還有%d分鐘進站，%s", first.Route, first.ETAMinutes, first.Direction)
	}
	return fmt.Sprintf("%s路目前無即時資訊，%s", first.Route, first.Direction)
}

// pickStation decides which of GeoAgent's candidates to lock onto, folding
// in VisionAgent's reading of the photo (if any). Vision can only ever
// pick among candidates GPS already found within radius — see
// agent.VisionAgent's doc comment for why that bound matters.
//
// Station name alone cannot break every tie: opposite-direction stops
// commonly share the exact same name (this project's own index turned up
// a real pair, "華中橋", 18m apart). For that case, direction label (e.g.
// "往板橋" vs "往撫遠街") is the signal that actually differs — but it must
// only be compared *within* the name-matched subset, not against every
// candidate in radius: a live test against the real index found a third,
// unrelated station 119m away that happened to share the "往板橋" label,
// which caused a false match when direction was checked pool-wide. Ambiguity
// scoped to two nearby doors of the same building beats a coincidental
// text match three buildings down.
func pickStation(bb *agent.Blackboard) {
	if len(bb.Candidates) == 0 {
		return
	}
	if bb.VisionFound {
		named := filterByName(bb.Candidates, bb.VisionStopName)
		switch len(named) {
		case 0:
			// Vision's reading didn't match any nearby station name — too
			// unreliable to also trust for direction matching against the
			// full (largely unrelated) pool, so fall through to nearest.
		case 1:
			lockStation(bb, named[0])
			return
		default:
			if c, ok := uniqueByDirection(named, bb.VisionDestinationText); ok {
				lockStation(bb, c)
				return
			}
			lockStation(bb, named[0]) // still narrowed by name; nearest among that group
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

// filterByName returns every candidate whose name matches visionName,
// preserving bb.Candidates' nearest-first order.
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

// stationNameMatches compares a scraped station name against Gemini's
// reading of the sign. Substring rather than exact equality because
// pda5284's station names sometimes carry a parenthetical suffix (e.g.
// "新北市政府(新府路)") that a photo reading may or may not include.
func stationNameMatches(indexed, vision string) bool {
	indexed, vision = strings.TrimSpace(indexed), strings.TrimSpace(vision)
	if indexed == "" || vision == "" {
		return false
	}
	return strings.Contains(indexed, vision) || strings.Contains(vision, indexed)
}

// decodeImage accepts either plain base64 or a data URI
// ("data:image/jpeg;base64,...") and returns the raw bytes plus a MIME
// type — from the data URI if present, otherwise sniffed from the bytes
// themselves. Returns (nil, "") for anything it can't decode, which
// VisionAgent treats the same as "no image submitted".
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
