




package pda




type Route struct {
	ID   string
	Name string
}



type StopRef struct {
	SID            int
	RouteID        string
	Direction      string 
	DirectionLabel string 
	Name           string
	Seq            int
}


type RouteDetail struct {
	RouteID   string
	Title     string
	GoLabel   string
	BackLabel string
	Stops     []StopRef
}



type StopETA struct {
	StopID         int
	RouteID        string
	ScheduledTime  string 
	ETASeconds     int    
	HasETA         bool
	RemainingStops int
	StatusCode     string
}



type BusPosition struct {
	VehicleID     string
	Lon, Lat      float64
	SpeedKmh      int
	CurrentStopID int 
}



type StopLocationResult struct {
	UpdateTime string
	Routes     []StopETA
}



type RouteDynaResult struct {
	UpdateTime string
	Stops      []StopETA
	Buses      []BusPosition
}
