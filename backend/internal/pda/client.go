package pda

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"time"
)

const baseURL = "https://pda5284.gov.taipei/MQS/"

// Client is rate-limited to be a polite scraper of a public, unauthenticated
// government site with no published quota.
type Client struct {
	http   *http.Client
	ticker *time.Ticker
}

// NewClient builds a client that self-limits to ~2 requests/second.
func NewClient() *Client {
	return &Client{
		http:   &http.Client{Timeout: 10 * time.Second},
		ticker: time.NewTicker(500 * time.Millisecond),
	}
}

// get fetches path and returns the final response body, following
// redirects (net/http's default client does this automatically — used by
// ResolveStopLocation, which depends on stop.jsp's 302).
func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	select {
	case <-c.ticker.C:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "devjam-accessibility-bus-assistant/1.0")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("pda: GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("pda: read body for %s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pda: GET %s: unexpected status %d", path, resp.StatusCode)
	}
	return body, nil
}

// StopLocationDyna fetches every route's live ETA at one aggregated station
// (slid). This is the endpoint the fast /api/analyze path calls.
func (c *Client) StopLocationDyna(ctx context.Context, slid int) (*StopLocationResult, error) {
	body, err := c.get(ctx, fmt.Sprintf("StopLocationDyna?stoplocationid=%d", slid))
	if err != nil {
		return nil, err
	}
	env, err := decodeDynaEnvelope(body)
	if err != nil {
		return nil, err
	}
	res := &StopLocationResult{UpdateTime: html.UnescapeString(env.UpdateTime)}
	for _, s := range env.Stop {
		if row, ok := parseDynaRow(s.N1); ok {
			res.Routes = append(res.Routes, row)
		}
	}
	return res, nil
}

// RouteDyna fetches live ETAs and vehicle GPS fixes for one entire route.
// cmd/indexer uses the vehicle fixes to infer station coordinates (see
// internal/stopindex), since pda5284 never publishes stop coordinates
// directly.
func (c *Client) RouteDyna(ctx context.Context, routeID string) (*RouteDynaResult, error) {
	body, err := c.get(ctx, fmt.Sprintf("RouteDyna?routeid=%s", routeID))
	if err != nil {
		return nil, err
	}
	env, err := decodeDynaEnvelope(body)
	if err != nil {
		return nil, err
	}
	res := &RouteDynaResult{UpdateTime: html.UnescapeString(env.UpdateTime)}
	for _, s := range env.Stop {
		if row, ok := parseDynaRow(s.N1); ok {
			res.Stops = append(res.Stops, row)
		}
	}
	for _, b := range env.Bus {
		lon, lat, speed, ok := parseBusA1(b.A1)
		if !ok {
			continue
		}
		pos := BusPosition{VehicleID: b.Num, Lon: lon, Lat: lat, SpeedKmh: speed}
		if sid, ok := parseBusA2StopID(b.A2); ok {
			pos.CurrentStopID = sid
		}
		res.Buses = append(res.Buses, pos)
	}
	return res, nil
}
