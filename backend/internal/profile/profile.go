// Package profile stores each user's accessibility preferences, matching
// the shape GET/POST /api/profile exchanges with the frontend.
package profile

// Profile is one user's accessibility preferences.
type Profile struct {
	ImpairmentType string   `json:"impairment_type"`
	SafeZone       SafeZone `json:"safe_zone"`
	FontScale      float64  `json:"font_scale"`
	VoiceEnabled   bool     `json:"voice_enabled"`
}

type SafeZone struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Radius float64 `json:"radius"`
}

// ReactionBufferMinutes is how much lead time this user needs to get from
// where they're standing to the curb and board safely. It directly drives
// the urgency thresholds in httpapi: the same ETA reads as more urgent for
// a profile that needs longer to react.
func (p Profile) ReactionBufferMinutes() int {
	switch p.ImpairmentType {
	case "tunnel_vision", "low_vision", "mobility":
		return 3
	default:
		return 1
	}
}
