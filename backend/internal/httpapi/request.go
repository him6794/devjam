package httpapi

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

const maxAnalyzeImageBytes = 10 << 20

type analyzeLocation struct {
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	AccuracyM float64 `json:"accuracy_m"`
}

type analyzeRequest struct {
	UserID   string          `json:"user_id"`
	Location analyzeLocation `json:"location"`
	Image    string          `json:"image"`
	
	
	WantedRoute string `json:"wanted_route"`
}

func parseAnalyzeRequest(c *gin.Context) (analyzeRequest, error) {
	contentType := strings.ToLower(c.GetHeader("Content-Type"))
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return parseMultipartAnalyzeRequest(c)
	}

	var request analyzeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		return analyzeRequest{}, fmt.Errorf("parse analyze JSON: %w", err)
	}
	return request, nil
}

func parseMultipartAnalyzeRequest(c *gin.Context) (analyzeRequest, error) {
	if err := c.Request.ParseMultipartForm(maxAnalyzeImageBytes); err != nil {
		return analyzeRequest{}, fmt.Errorf("parse analyze multipart form: %w", err)
	}

	lat, err := parseMultipartFloat(c.PostForm("lat"), "lat")
	if err != nil {
		return analyzeRequest{}, err
	}
	lng, err := parseMultipartFloat(c.PostForm("lng"), "lng")
	if err != nil {
		return analyzeRequest{}, err
	}

	file, header, err := c.Request.FormFile("image")
	if err != nil {
		return analyzeRequest{}, fmt.Errorf("read analyze image: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxAnalyzeImageBytes+1))
	if err != nil {
		return analyzeRequest{}, fmt.Errorf("read analyze image: %w", err)
	}
	if len(data) > maxAnalyzeImageBytes {
		return analyzeRequest{}, fmt.Errorf("analyze image exceeds %d bytes", maxAnalyzeImageBytes)
	}

	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = http.DetectContentType(data)
	}

	return analyzeRequest{
		UserID: c.PostForm("user_id"),
		Location: analyzeLocation{
			Lat:       lat,
			Lng:       lng,
			AccuracyM: parseOptionalMultipartFloat(c.PostForm("accuracy_m")),
		},
		Image:       "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data),
		WantedRoute: c.PostForm("wanted_route"),
	}, nil
}

func parseMultipartFloat(raw, field string) (float64, error) {
	if raw == "" {
		return 0, fmt.Errorf("location.%s is required", field)
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("parse location.%s: %w", field, err)
	}
	return value, nil
}

func parseOptionalMultipartFloat(raw string) float64 {
	if raw == "" {
		return 0
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return value
}
