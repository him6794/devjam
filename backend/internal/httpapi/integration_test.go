package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"devjam-backend/internal/profile"
)

func Test_ParseAnalyzeRequest_accepts_multipart_form(t *testing.T) {
	
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("user_id", "rider-1"); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("lat", "25.051717"); err != nil {
		t.Fatal(err)
	}
	if err := form.WriteField("lng", "121.552853"); err != nil {
		t.Fatal(err)
	}
	part, err := form.CreateFormFile("image", "frame.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("jpeg-bytes")); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/analyze", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = req

	
	got, err := parseAnalyzeRequest(ctx)

	
	if err != nil {
		t.Fatal(err)
	}
	if got.UserID != "rider-1" || got.Location.Lat != 25.051717 || got.Location.Lng != 121.552853 {
		t.Fatalf("unexpected request: %+v", got)
	}
	if !strings.HasPrefix(got.Image, "data:") || !strings.Contains(got.Image, ";base64,") {
		t.Fatalf("image was not converted to a data URI: %q", got.Image)
	}
}

func Test_ProfilePost_persists_body_user_id_for_get(t *testing.T) {
	
	handler := NewProfileHandler(profile.NewStore())
	router := gin.New()
	router.POST("/api/profile", handler.Post)
	router.GET("/api/profile", handler.Get)

	post := httptest.NewRequest(http.MethodPost, "/api/profile", strings.NewReader(`{
		"user_id":"rider-1",
		"impairment_type":"tunnel_vision",
		"safe_zone":{"x":50,"y":20,"radius":30},
		"font_scale":1.5,
		"voice_enabled":true
	}`))
	post.Header.Set("Content-Type", "application/json")
	postResponse := httptest.NewRecorder()

	
	router.ServeHTTP(postResponse, post)

	
	if postResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/profile status = %d", postResponse.Code)
	}
	get := httptest.NewRequest(http.MethodGet, "/api/profile", nil)
	get.Header.Set("X-User-Id", "rider-1")
	getResponse := httptest.NewRecorder()
	router.ServeHTTP(getResponse, get)
	if getResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/profile status = %d", getResponse.Code)
	}

	var payload struct {
		Exists         bool    `json:"exists"`
		ImpairmentType string  `json:"impairment_type"`
		FontScale      float64 `json:"font_scale"`
	}
	if err := json.NewDecoder(getResponse.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Exists || payload.ImpairmentType != "tunnel_vision" || payload.FontScale != 1.5 {
		t.Fatalf("unexpected profile response: %+v", payload)
	}
}

func Test_AlertHandlers_create_and_list_by_route(t *testing.T) {
	
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	store := newAlertStore(func() time.Time { return now })
	router := gin.New()
	router.POST("/api/notify_driver", newNotifyHandler(store))
	router.GET("/api/driver_alerts", newAlertsHandler(store))

	request := httptest.NewRequest(http.MethodPost, "/api/notify_driver", strings.NewReader(`{
		"route":"307",
		"station_name":"捷運公館站",
		"impairment_type":"tunnel_vision"
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	
	router.ServeHTTP(response, request)

	
	if response.Code != http.StatusOK {
		t.Fatalf("POST /api/notify_driver status = %d", response.Code)
	}
	listResponse := httptest.NewRecorder()
	listRequest := httptest.NewRequest(http.MethodGet, "/api/driver_alerts?route=307", nil)
	router.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/driver_alerts status = %d", listResponse.Code)
	}
	var payload struct {
		Alerts []struct {
			AlertID        string `json:"alert_id"`
			Route          string `json:"route"`
			StationName    string `json:"station_name"`
			ImpairmentType string `json:"impairment_type"`
		} `json:"alerts"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Alerts) != 1 || payload.Alerts[0].AlertID == "" || payload.Alerts[0].Route != "307" || payload.Alerts[0].StationName != "捷運公館站" || payload.Alerts[0].ImpairmentType != "tunnel_vision" {
		t.Fatalf("unexpected alert response: %+v", payload.Alerts)
	}
}
