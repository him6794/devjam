package pda

import (
	"encoding/json"
	"fmt"
	"html"
	"strconv"
	"strings"
)

type stopRow struct {
	ID int    `json:"id"`
	N1 string `json:"n1"`
}

type busRow struct {
	Num string `json:"num"`
	A1  string `json:"a1"`
	A2  string `json:"a2"`
	A3  string `json:"a3"`
}

type dynaEnvelope struct {
	UpdateTime string    `json:"UpdateTime"`
	Stop       []stopRow `json:"Stop"`
	Bus        []busRow  `json:"Bus"`
}

func decodeDynaEnvelope(body []byte) (*dynaEnvelope, error) {
	var env dynaEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("pda: decode dyna response: %w", err)
	}
	return &env, nil
}









func parseDynaRow(raw string) (StopETA, bool) {
	f := strings.Split(raw, ",")
	if len(f) < 10 || f[0] != "N1" {
		return StopETA{}, false
	}
	sid, err := strconv.Atoi(f[1])
	if err != nil {
		return StopETA{}, false
	}
	row := StopETA{StopID: sid, RouteID: f[2], ETASeconds: -1, StatusCode: f[9]}
	if strings.Contains(f[3], ":") {
		row.ScheduledTime = html.UnescapeString(f[3])
	}
	if eta, err := strconv.Atoi(f[7]); err == nil && eta >= 0 {
		row.ETASeconds = eta
		row.HasETA = true
	}
	if n, err := strconv.Atoi(f[8]); err == nil {
		row.RemainingStops = n
	}
	return row, true
}





func parseBusA1(raw string) (lon, lat float64, speedKmh int, ok bool) {
	f := strings.Split(raw, ",")
	if len(f) < 10 {
		return 0, 0, 0, false
	}
	var errLon, errLat, errSpeed error
	lon, errLon = strconv.ParseFloat(f[7], 64)
	lat, errLat = strconv.ParseFloat(f[8], 64)
	speedKmh, errSpeed = strconv.Atoi(f[9])
	if errLon != nil || errLat != nil || errSpeed != nil {
		return 0, 0, 0, false
	}
	return lon, lat, speedKmh, true
}





func parseBusA2StopID(raw string) (int, bool) {
	f := strings.Split(raw, ",")
	if len(f) < 8 {
		return 0, false
	}
	id, err := strconv.Atoi(f[7])
	if err != nil {
		return 0, false
	}
	return id, true
}
