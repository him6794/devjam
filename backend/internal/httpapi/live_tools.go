package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"google.golang.org/genai"

	"devjam-backend/internal/agent"
	"devjam-backend/internal/skill"
)






type StationStatusIn struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type stationCandidate struct {
	SlID            int      `json:"slid"`
	Name            string   `json:"name"`
	DistanceM       float64  `json:"distance_m"`
	DirectionLabels []string `json:"direction_labels"`
}

type stationStatusOut struct {
	Found      bool               `json:"found"`
	Message    string             `json:"message,omitempty"`
	SlID       int                `json:"slid,omitempty"`
	Name       string             `json:"station_name,omitempty"`
	DistanceM  float64            `json:"distance_m,omitempty"`
	Candidates []stationCandidate `json:"candidates,omitempty"`
	Buses      []skill.BusETA     `json:"buses,omitempty"`
}







type StationStatusSkill struct {
	orchestrator *agent.Orchestrator
	nearestStops *skill.NearestStops
	stopETA      *skill.StopETA
}

func NewStationStatusSkill(o *agent.Orchestrator, nearest *skill.NearestStops, eta *skill.StopETA) *StationStatusSkill {
	return &StationStatusSkill{orchestrator: o, nearestStops: nearest, stopETA: eta}
}

func (s *StationStatusSkill) Name() string { return "station_status" }

func (s *StationStatusSkill) Description() string {
	return "Run the agent pipeline (nearest station lookup, then live ETAs) for a GPS position and return the station and its buses."
}

func (s *StationStatusSkill) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"lat": map[string]any{"type": "number", "description": "latitude, WGS84; omitted in live sessions, filled from the rider's current GPS"},
			"lon": map[string]any{"type": "number", "description": "longitude, WGS84; omitted in live sessions, filled from the rider's current GPS"},
		},
	}
}

func (s *StationStatusSkill) Do(ctx context.Context, in StationStatusIn) (stationStatusOut, error) {
	if in.Lat == 0 && in.Lon == 0 {
		return stationStatusOut{}, errors.New("station_status: 缺少定位資訊")
	}
	bb := &agent.Blackboard{Lat: in.Lat, Lon: in.Lon}

	
	
	
	s.orchestrator.RunWave(ctx, bb, agent.NewGeoAgent(s.nearestStops))
	if len(bb.Candidates) == 0 || bb.Candidates[0].Distance > farAwayThresholdM {
		return stationStatusOut{Found: false, Message: "附近150公尺內找不到任何已知站牌"}, nil
	}
	pickStation(bb)

	n := len(bb.Candidates)
	if n > 3 {
		n = 3
	}
	candidates := make([]stationCandidate, 0, n)
	for _, c := range bb.Candidates[:n] {
		candidates = append(candidates, stationCandidate{
			SlID: c.SlID, Name: c.Name, DistanceM: c.Distance, DirectionLabels: c.DirectionLabels,
		})
	}

	
	s.orchestrator.RunWave(ctx, bb, agent.NewTransitAgent(s.stopETA))

	buses := make([]skill.BusETA, 0, len(bb.Buses))
	for _, b := range bb.Buses {
		buses = append(buses, skill.BusETA{
			Route: b.Route, Direction: b.Direction, ETAMinutes: b.ETAMinutes, HasETA: b.HasETA,
		})
	}
	sort.SliceStable(buses, func(i, j int) bool {
		if buses[i].HasETA != buses[j].HasETA {
			return buses[i].HasETA
		}
		return buses[i].ETAMinutes < buses[j].ETAMinutes
	})

	return stationStatusOut{
		Found:      true,
		SlID:       bb.NearestSlID,
		Name:       bb.NearestName,
		DistanceM:  bb.NearestDistM,
		Candidates: candidates,
		Buses:      buses,
	}, nil
}

func (s *StationStatusSkill) Invoke(ctx context.Context, raw json.RawMessage) (any, error) {
	var in StationStatusIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("skill %s: bad args: %w", s.Name(), err)
	}
	return s.Do(ctx, in)
}





type liveToolset struct {
	stationStatus *StationStatusSkill
	stopETA       *skill.StopETA
}

func NewLiveToolset(stationStatus *StationStatusSkill, stopETA *skill.StopETA) *liveToolset {
	return &liveToolset{stationStatus: stationStatus, stopETA: stopETA}
}

func (t *liveToolset) Functions() []*genai.FunctionDeclaration {
	return []*genai.FunctionDeclaration{
		{
			Name: "station_status",
			Description: "查詢使用者目前位置附近站牌的公車即時到站時間。直接呼叫、不要帶參數，系統會自動代入使用者目前的 GPS 位置。" +
				"回傳最近站牌的名稱、slid、附近其他候選站牌（candidates）與各路線到站分鐘數（buses）。",
		},
		{
			Name:        "stop_eta",
			Description: "查詢特定公車站牌所有路線的即時到站時間（分鐘）。",
			Parameters:  skill.SchemaFromMap(t.stopETA.InputSchema()),
		},
	}
}

func (t *liveToolset) Invoke(ctx context.Context, name string, args map[string]any, gps *skill.GPSCoord) (any, error) {
	switch name {
	case "station_status":
		if args == nil {
			args = map[string]any{}
		}
		
		
		if _, ok := args["lat"]; !ok {
			if gps == nil {
				return nil, errors.New("station_status: 尚無定位資訊，請稍後再試")
			}
			args["lat"], args["lon"] = gps.Lat, gps.Lon
		} else if _, ok := args["lon"]; !ok {
			if gps == nil {
				return nil, errors.New("station_status: 尚無定位資訊，請稍後再試")
			}
			args["lon"] = gps.Lon
		}
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("station_status: marshal args: %w", err)
		}
		return t.stationStatus.Invoke(ctx, raw)
	case "stop_eta":
		raw, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("stop_eta: marshal args: %w", err)
		}
		return t.stopETA.Invoke(ctx, raw)
	default:
		return nil, fmt.Errorf("live tools: unknown tool %q", name)
	}
}
