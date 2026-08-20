





package agent

import (
	"context"

	"devjam-backend/internal/profile"
)




type Blackboard struct {
	Lat, Lon  float64
	ImageData []byte
	ImageMIME string

	
	
	
	
	
	Candidates []Candidate

	VisionFound           bool
	VisionStopName        string
	VisionDestinationText string

	NearestFound bool
	NearestSlID  int
	NearestName  string
	NearestDistM float64

	
	
	
	
	WantedRoute string

	Profile ProfileView

	Buses []BusReport
}


type Candidate struct {
	SlID            int
	Name            string
	Distance        float64
	DirectionLabels []string 
}



type ProfileView struct {
	Exists            bool
	ImpairmentType    string
	ReactionBufferMin int
	FontScale         float64
	SafeZone          profile.SafeZone
}



type BusReport struct {
	Route      string
	Direction  string
	ETAMinutes int
	HasETA     bool
}





type Contribution struct {
	Agent    string
	Apply    func(*Blackboard)
	Degraded bool
	Err      error
}


type Agent interface {
	Name() string
	Run(ctx context.Context, bb Blackboard) Contribution
}
