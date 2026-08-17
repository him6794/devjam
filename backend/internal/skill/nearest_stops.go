package skill

import (
	"context"
	"encoding/json"
	"fmt"

	"devjam-backend/internal/stopindex"
)

type NearestStopsIn struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	RadiusM float64 `json:"radius_m"`
}

type NearestStopsOut struct {
	Matches []stopindex.Match `json:"matches"`
}

// NearestStops finds known bus stations near a GPS coordinate. It is pure,
// in-memory computation — no network call — which is why it is safe to run
// on every /api/analyze request instead of only in the async agent path.
type NearestStops struct {
	index *stopindex.Index
}

func NewNearestStops(index *stopindex.Index) *NearestStops {
	return &NearestStops{index: index}
}

func (s *NearestStops) Name() string { return "nearest_stops" }

func (s *NearestStops) Description() string {
	return "Find known bus stations within a radius (meters) of a GPS coordinate, nearest first."
}

func (s *NearestStops) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"lat":      map[string]any{"type": "number", "description": "latitude, WGS84"},
			"lon":      map[string]any{"type": "number", "description": "longitude, WGS84"},
			"radius_m": map[string]any{"type": "number", "description": "search radius in meters", "default": 150},
		},
		"required": []string{"lat", "lon"},
	}
}

func (s *NearestStops) Do(ctx context.Context, in NearestStopsIn) (NearestStopsOut, error) {
	radius := in.RadiusM
	if radius <= 0 {
		radius = 150
	}
	return NearestStopsOut{Matches: s.index.Nearest(in.Lat, in.Lon, radius)}, nil
}

func (s *NearestStops) Invoke(ctx context.Context, raw json.RawMessage) (any, error) {
	var in NearestStopsIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("skill %s: bad args: %w", s.Name(), err)
	}
	return s.Do(ctx, in)
}
