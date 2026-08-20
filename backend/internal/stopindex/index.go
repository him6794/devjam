






package stopindex

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)



type Station struct {
	SlID int     `json:"slid"`
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}



type Stop struct {
	SID            int    `json:"sid"`
	SlID           int    `json:"slid"`
	RouteID        string `json:"route_id"`       
	RouteName      string `json:"route_name"`     
	Direction      string `json:"direction"`       
	DirectionLabel string `json:"direction_label"` 
	StopName       string `json:"stop_name"`
}


type File struct {
	GeneratedAt time.Time `json:"generated_at"`
	Stations    []Station `json:"stations"`
	Stops       []Stop    `json:"stops"`
}





type Index struct {
	stations         []Station
	metaBySID        map[int]Stop
	directionsBySlID map[int][]string 
	routesBySlID     map[int][]Stop   
}

func Load(path string) (*Index, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("stopindex: read %s: %w", path, err)
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("stopindex: parse %s: %w", path, err)
	}
	return newIndex(f), nil
}

func newIndex(f File) *Index {
	idx := &Index{
		stations:         f.Stations,
		metaBySID:        make(map[int]Stop, len(f.Stops)),
		directionsBySlID: make(map[int][]string),
		routesBySlID:     make(map[int][]Stop),
	}
	seen := make(map[int]map[string]bool)
	seenRoute := make(map[int]map[string]bool) 
	for _, s := range f.Stops {
		idx.metaBySID[s.SID] = s
		if seenRoute[s.SlID] == nil {
			seenRoute[s.SlID] = map[string]bool{}
		}
		routeKey := s.RouteName + "|" + s.Direction
		if !seenRoute[s.SlID][routeKey] {
			seenRoute[s.SlID][routeKey] = true
			idx.routesBySlID[s.SlID] = append(idx.routesBySlID[s.SlID], s)
		}
		if s.DirectionLabel == "" {
			continue
		}
		if seen[s.SlID] == nil {
			seen[s.SlID] = map[string]bool{}
		}
		if !seen[s.SlID][s.DirectionLabel] {
			seen[s.SlID][s.DirectionLabel] = true
			idx.directionsBySlID[s.SlID] = append(idx.directionsBySlID[s.SlID], s.DirectionLabel)
		}
	}
	return idx
}


type Match struct {
	Station         Station  `json:"station"`
	Distance        float64  `json:"distance_m"`
	DirectionLabels []string `json:"direction_labels,omitempty"`
}








func (idx *Index) Nearest(lat, lon, radiusM float64) []Match {
	var matches []Match
	for _, st := range idx.stations {
		d := haversineMeters(lat, lon, st.Lat, st.Lon)
		if d <= radiusM {
			matches = append(matches, Match{Station: st, Distance: d, DirectionLabels: idx.directionsBySlID[st.SlID]})
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].Distance < matches[j].Distance })
	return matches
}



func (idx *Index) LookupStop(sid int) (Stop, bool) {
	s, ok := idx.metaBySID[sid]
	return s, ok
}





func (idx *Index) RoutesAt(slid int) []Stop {
	return idx.routesBySlID[slid]
}




func (idx *Index) StationByID(slid int) (Station, bool) {
	for _, st := range idx.stations {
		if st.SlID == slid {
			return st, true
		}
	}
	return Station{}, false
}





func (idx *Index) FindStationByName(name string) (Station, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Station{}, false
	}
	for _, st := range idx.stations {
		if strings.Contains(st.Name, name) || strings.Contains(name, st.Name) {
			return st, true
		}
	}
	return Station{}, false
}

func (idx *Index) Len() int { return len(idx.stations) }





func (idx *Index) StationNames() []string {
	names := make([]string, len(idx.stations))
	for i, st := range idx.stations {
		names[i] = st.Name
	}
	return names
}

const earthRadiusM = 6371000

func haversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	toRad := func(deg float64) float64 { return deg * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLon := toRad(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusM * c
}
