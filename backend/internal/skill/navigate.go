package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/genai"

	"devjam-backend/internal/stopindex"
)




const navigateSearchRadiusM = 150

type NavigateIn struct {
	
	OriginLat float64 `json:"origin_lat"`
	OriginLon float64 `json:"origin_lon"`
	
	Destination string `json:"destination"`
}



type NavigateStep struct {
	Kind string `json:"kind"` 
	Text string `json:"text"` 
}

type NavigateOut struct {
	Found bool `json:"found"`
	
	
	
	
	OriginStationName      string         `json:"origin_station_name"`
	DestinationStationName string         `json:"destination_station_name"`
	Route                  string         `json:"route,omitempty"`     
	Direction               string        `json:"direction,omitempty"` 
	Steps                  []NavigateStep `json:"steps"`
	Message                string         `json:"message,omitempty"` 
}




var destinationSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"found":       {Type: genai.TypeBoolean},
		"station_name": {Type: genai.TypeString},
	},
	Required: []string{"found", "station_name"},
}

const destinationPrompt = `使用者說出想去的目的地，請從下面「已知站名清單」中，選出最符合使用者說法的一個站名。
使用者說法可能是口語、簡稱、或地標名稱（例如「車站」可能指「台北車站」、「公館」指「公館站」）。
只能選清單中已存在的站名，不可自創；如果清單中沒有任何合理對應，found 設為 false。

已知站名清單：
%s

使用者說的目的地：「%s」`











type Navigate struct {
	index  *stopindex.Index
	genai  *genai.Client
	model  string
}

func NewNavigate(index *stopindex.Index, genaiClient *genai.Client) *Navigate {
	return &Navigate{index: index, genai: genaiClient, model: "gemini-2.5-flash"}
}

func (s *Navigate) Name() string { return "navigate" }

func (s *Navigate) Description() string {
	return "Plan a walk-to-stop, board, ride, alight route from the rider's GPS to a spoken/typed destination, using known stations and routes."
}

func (s *Navigate) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"origin_lat":  map[string]any{"type": "number"},
			"origin_lon":  map[string]any{"type": "number"},
			"destination": map[string]any{"type": "string", "description": "spoken or typed destination, e.g. 台北車站"},
		},
		"required": []string{"origin_lat", "origin_lon", "destination"},
	}
}

func (s *Navigate) Do(ctx context.Context, in NavigateIn) (NavigateOut, error) {
	dest := strings.TrimSpace(in.Destination)
	if dest == "" {
		return NavigateOut{}, errors.New("navigate: empty destination")
	}

	originMatches := s.index.Nearest(in.OriginLat, in.OriginLon, navigateSearchRadiusM)
	if len(originMatches) == 0 {
		return NavigateOut{Found: false, Message: "附近找不到公車站，請先走到站牌附近"}, nil
	}
	origin := originMatches[0].Station

	destStation, ok := s.resolveDestination(ctx, dest)
	if !ok {
		return NavigateOut{Found: false, Message: fmt.Sprintf("找不到「%s」對應的公車站，請換個說法或說出更明確的站名", dest)}, nil
	}

	if destStation.SlID == origin.SlID {
		return NavigateOut{
			Found:                   true,
			OriginStationName:       origin.Name,
			DestinationStationName:  destStation.Name,
			Steps: []NavigateStep{
				{Kind: "walk_final", Text: fmt.Sprintf("您輸入的目的地「%s」就是您現在所在的站，不需要搭車", destStation.Name)},
			},
		}, nil
	}

	route, direction, ok := s.findDirectRoute(origin.SlID, destStation.SlID)
	if !ok {
		return NavigateOut{
			Found:                  false,
			OriginStationName:      origin.Name,
			DestinationStationName: destStation.Name,
			Message:                fmt.Sprintf("目前資料庫中沒有直達「%s」到「%s」的路線，可能需要轉乘，建議到站後詢問站務或司機", origin.Name, destStation.Name),
		}, nil
	}

	
	
	
	steps := []NavigateStep{
		{Kind: "walk", Text: fmt.Sprintf("請走到「%s」站牌", origin.Name)},
		{Kind: "board", Text: fmt.Sprintf("上車前確認車頭或車身顯示「%s」，方向為「%s」", route, direction)},
		{Kind: "ride", Text: fmt.Sprintf("搭乘%s路，%s方向", route, direction)},
		{Kind: "alight", Text: fmt.Sprintf("到「%s」站下車", destStation.Name)},
	}
	return NavigateOut{
		Found:                  true,
		OriginStationName:      origin.Name,
		DestinationStationName: destStation.Name,
		Route:                  route,
		Direction:              direction,
		Steps:                  steps,
	}, nil
}







func (s *Navigate) resolveDestination(ctx context.Context, dest string) (stopindex.Station, bool) {
	if st, ok := s.index.FindStationByName(dest); ok {
		return st, true
	}
	if s.genai == nil {
		return stopindex.Station{}, false
	}

	names := s.index.StationNames()
	if len(names) == 0 {
		return stopindex.Station{}, false
	}
	resp, err := s.genai.Models.GenerateContent(ctx, s.model,
		[]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{Text: fmt.Sprintf(destinationPrompt, strings.Join(names, "、"), dest)},
			},
		}},
		&genai.GenerateContentConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   destinationSchema,
		},
	)
	if err != nil {
		return stopindex.Station{}, false
	}
	var out struct {
		Found       bool   `json:"found"`
		StationName string `json:"station_name"`
	}
	if err := json.Unmarshal([]byte(resp.Text()), &out); err != nil || !out.Found {
		return stopindex.Station{}, false
	}
	return s.index.FindStationByName(out.StationName)
}










func (s *Navigate) findDirectRoute(originSlID, destSlID int) (route, direction string, ok bool) {
	originRoutes := s.index.RoutesAt(originSlID)
	destRoutes := s.index.RoutesAt(destSlID)
	if len(originRoutes) == 0 || len(destRoutes) == 0 {
		return "", "", false
	}
	destSet := make(map[string]bool, len(destRoutes))
	for _, r := range destRoutes {
		destSet[r.RouteName] = true
	}
	for _, r := range originRoutes {
		if r.RouteName != "" && destSet[r.RouteName] {
			return r.RouteName, r.DirectionLabel, true
		}
	}
	return "", "", false
}

func (s *Navigate) Invoke(ctx context.Context, raw json.RawMessage) (any, error) {
	var in NavigateIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("skill %s: bad args: %w", s.Name(), err)
	}
	return s.Do(ctx, in)
}
