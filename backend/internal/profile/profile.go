

package profile


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





func (p Profile) ReactionBufferMinutes() int {
	switch p.ImpairmentType {
	case "tunnel_vision", "low_vision", "mobility":
		return 3
	default:
		return 1
	}
}
