package skill

import (
	"context"
	"encoding/json"
	"fmt"

	"devjam-backend/internal/pda"
	"devjam-backend/internal/stopindex"
)

type StopETAIn struct {
	SlID int `json:"slid"`
}

type BusETA struct {
	Route      string `json:"route"`
	Direction  string `json:"direction"`
	ETAMinutes int    `json:"eta_minutes"`
	HasETA     bool   `json:"has_eta"`
}

type StopETAOut struct {
	UpdateTime string   `json:"update_time"`
	Buses      []BusETA `json:"buses"`
}




type StopETA struct {
	pda   *pda.Client
	index *stopindex.Index
}

func NewStopETA(client *pda.Client, index *stopindex.Index) *StopETA {
	return &StopETA{pda: client, index: index}
}

func (s *StopETA) Name() string { return "stop_eta" }

func (s *StopETA) Description() string {
	return "Get every route's live ETA (minutes) at one bus station, identified by its station id (slid)."
}

func (s *StopETA) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"slid": map[string]any{"type": "integer", "description": "station id from nearest_stops"}},
		"required":   []string{"slid"},
	}
}

func (s *StopETA) Do(ctx context.Context, in StopETAIn) (StopETAOut, error) {
	live, err := s.pda.StopLocationDyna(ctx, in.SlID)
	if err != nil {
		return StopETAOut{}, err
	}
	out := StopETAOut{UpdateTime: live.UpdateTime}
	for _, row := range live.Routes {
		route, direction := row.RouteID, ""
		if meta, ok := s.index.LookupStop(row.StopID); ok {
			route, direction = meta.RouteName, meta.DirectionLabel
		}
		bus := BusETA{Route: route, Direction: direction, HasETA: row.HasETA}
		if row.HasETA {
			bus.ETAMinutes = (row.ETASeconds + 59) / 60 
		}
		out.Buses = append(out.Buses, bus)
	}
	return out, nil
}

func (s *StopETA) Invoke(ctx context.Context, raw json.RawMessage) (any, error) {
	var in StopETAIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("skill %s: bad args: %w", s.Name(), err)
	}
	return s.Do(ctx, in)
}
