package httpapi

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type driverAlert struct {
	AlertID        string    `json:"alert_id"`
	Route          string    `json:"route"`
	StationName    string    `json:"station_name"`
	ImpairmentType string    `json:"impairment_type"`
	Timestamp      time.Time `json:"timestamp"`
	Acknowledged   bool      `json:"acknowledged"`
}

type alertStore struct {
	mu     sync.RWMutex
	now    func() time.Time
	nextID uint64
	alerts []driverAlert
}

func newAlertStore(now func() time.Time) *alertStore {
	return &alertStore{now: now}
}

func (s *alertStore) add(route, stationName, impairmentType string) driverAlert {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextID++
	alert := driverAlert{
		AlertID:        "alert-" + formatAlertID(s.nextID),
		Route:          route,
		StationName:    stationName,
		ImpairmentType: impairmentType,
		Timestamp:      s.now().UTC(),
	}
	s.alerts = append(s.alerts, alert)
	return alert
}

func (s *alertStore) active(route string) []driverAlert {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cutoff := s.now().Add(-10 * time.Minute)
	alerts := make([]driverAlert, 0, len(s.alerts))
	for _, alert := range s.alerts {
		if alert.Route == route && !alert.Timestamp.Before(cutoff) {
			alerts = append(alerts, alert)
		}
	}
	sort.SliceStable(alerts, func(i, j int) bool {
		return alerts[i].Timestamp.After(alerts[j].Timestamp)
	})
	return alerts
}

func formatAlertID(id uint64) string {
	return strconv.FormatUint(id, 10)
}

type notifyRequest struct {
	Route          string `json:"route"`
	StationName    string `json:"station_name"`
	ImpairmentType string `json:"impairment_type"`
}

func newNotifyHandler(store *alertStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		var request notifyRequest
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		request.Route = strings.TrimSpace(request.Route)
		if request.Route == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "route is required"})
			return
		}
		alert := store.add(request.Route, strings.TrimSpace(request.StationName), strings.TrimSpace(request.ImpairmentType))
		c.JSON(http.StatusOK, gin.H{"status": "success", "alert_id": alert.AlertID})
	}
}

func newAlertsHandler(store *alertStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		route := strings.TrimSpace(c.Query("route"))
		if route == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "route is required"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"alerts": store.active(route)})
	}
}
