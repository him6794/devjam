// Package pda talks to the Taipei city bus dynamic-information system at
// pda5284.gov.taipei/MQS/. It is an unofficial, undocumented feed reverse
// engineered for this project: there is no API key, no SLA, and no
// guaranteed schema. Every parser in this package is written to degrade
// (skip a malformed row) rather than fail the whole call.
package pda

// Route is one bus route as listed by the site's autocomplete box. ID is
// not always numeric (some intercity routes use their own code, e.g.
// "232快"), so it is kept as a string throughout this package.
type Route struct {
	ID   string
	Name string
}

// StopRef identifies one physical stop pole for one route in one
// direction, as scraped from route.jsp.
type StopRef struct {
	SID            int
	RouteID        string
	Direction      string // "go" or "back"
	DirectionLabel string // destination shown on the sign, e.g. "往台北車站"
	Name           string
	Seq            int
}

// RouteDetail is the parsed result of route.jsp for one route.
type RouteDetail struct {
	RouteID   string
	Title     string
	GoLabel   string
	BackLabel string
	Stops     []StopRef
}

// StopETA is one route's live status at a stop, decoded from an
// "N1,sid,rid,..." row returned by RouteDyna / StopLocationDyna.
type StopETA struct {
	StopID         int
	RouteID        string
	ScheduledTime  string // "HH:MM", set only when the bus has not departed yet
	ETASeconds     int    // -1 when there is no upcoming bus
	HasETA         bool
	RemainingStops int
	StatusCode     string
}

// BusPosition is one vehicle's live GPS fix, decoded from a paired
// Bus[].a1/a2 row.
type BusPosition struct {
	VehicleID     string
	Lon, Lat      float64
	SpeedKmh      int
	CurrentStopID int // 0 if the a2 row was missing or malformed
}

// StopLocationResult is the decoded response of StopLocationDyna: every
// route's live status at one aggregated station (slid).
type StopLocationResult struct {
	UpdateTime string
	Routes     []StopETA
}

// RouteDynaResult is the decoded response of RouteDyna: one route's live
// stop statuses plus every vehicle currently running it.
type RouteDynaResult struct {
	UpdateTime string
	Stops      []StopETA
	Buses      []BusPosition
}
