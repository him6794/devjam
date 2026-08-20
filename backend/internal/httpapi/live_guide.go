package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"devjam-backend/internal/skill"
)


















type LiveGuideHandler struct {
	guide *skill.LiveGuide
	
	
	
	
	stationStatus *StationStatusSkill
	upgrader      websocket.Upgrader
}

func NewLiveGuideHandler(guide *skill.LiveGuide, stationStatus *StationStatusSkill) *LiveGuideHandler {
	
	
	
	
	
	allowedHosts := map[string]bool{
		"devjam.justin0711.com": true,
		"localhost":             true,
		"127.0.0.1":             true,
	}
	if extra := os.Getenv("LIVE_GUIDE_ALLOWED_ORIGINS"); extra != "" {
		for _, o := range strings.Split(extra, ",") {
			o = strings.TrimSpace(o)
			if u, err := url.Parse(o); err == nil && u.Hostname() != "" {
				allowedHosts[u.Hostname()] = true
			}
		}
	}
	return &LiveGuideHandler{
		guide:         guide,
		stationStatus: stationStatus,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				
				
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true
				}
				u, err := url.Parse(origin)
				return err == nil && allowedHosts[u.Hostname()]
			},
		},
	}
}

const liveGuideFrameDeadline = 15 * time.Second









const liveGuideJudgmentInterval = 4 * time.Second

func (h *LiveGuideHandler) Handle(c *gin.Context) {
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("live_guide: websocket upgrade failed: %v", err)
		return
	}
	defer conn.Close()

	writeMu := &sync.Mutex{}
	writeText := func(text string) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.TextMessage, []byte(text))
	}

	
	
	
	
	
	
	
	done := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(done); conn.Close() }) }

	session, err := h.guide.Open(c.Request.Context(), skill.LiveCallbacks{
		OnText: func(text string) {
			if err := writeText(text); err != nil {
				stop()
			}
		},
		OnDone: func(err error) {
			log.Printf("live_guide: session ended: %v", err)
			stop()
		},
	})
	if err != nil {
		log.Printf("live_guide: open session failed: %v", err)
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "live_guide unavailable"))
		return
	}
	defer session.Close()

	
	
	
	
	
	
	
	
	
	
	
	go func() {
		defer stop()
		for {
			_ = conn.SetReadDeadline(time.Now().Add(liveGuideFrameDeadline))
			msgType, data, err := conn.ReadMessage()
			if err != nil {
				return 
			}
			if msgType != websocket.BinaryMessage || len(data) == 0 {
				continue
			}
			switch data[0] {
			case 0x00:
				if err := session.PushFrame(c.Request.Context(), data[1:]); err != nil {
					log.Printf("live_guide: push frame failed: %v", err)
					return
				}
			case 0x01:
				if err := session.PushAudio(c.Request.Context(), data[1:]); err != nil {
					log.Printf("live_guide: push audio failed: %v", err)
					return
				}
			case 0x02:
				var fix struct {
					Lat float64 `json:"lat"`
					Lng float64 `json:"lng"`
				}
				if err := json.Unmarshal(data[1:], &fix); err != nil {
					log.Printf("live_guide: bad GPS payload: %v", err)
					continue
				}
				session.SetGPS(fix.Lat, fix.Lng)
			default:
				
				if err := session.PushFrame(c.Request.Context(), data); err != nil {
					log.Printf("live_guide: push frame failed: %v", err)
					return
				}
			}
		}
	}()

	go func() {
		defer stop()
		ticker := time.NewTicker(liveGuideJudgmentInterval)
		defer ticker.Stop()
		
		
		
		
		const maxConsecutiveErrors = 3
		consecutiveErrors := 0
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				info := h.backgroundStationInfo(c.Request.Context(), session)
				if err := session.RequestJudgment(c.Request.Context(), info); err != nil {
					consecutiveErrors++
					log.Printf("live_guide: request judgment failed (%d/%d): %v", consecutiveErrors, maxConsecutiveErrors, err)
					if consecutiveErrors >= maxConsecutiveErrors {
						log.Printf("live_guide: too many consecutive judgment errors, closing session")
						return
					}
					continue
				}
				consecutiveErrors = 0 
			}
		}
	}()

	<-done
}











func (h *LiveGuideHandler) backgroundStationInfo(ctx context.Context, sess *skill.LiveGuideSession) string {
	if h.stationStatus == nil {
		return ""
	}
	gps := sess.GPS()
	if gps == nil {
		return ""
	}
	qctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := h.stationStatus.Do(qctx, StationStatusIn{Lat: gps.Lat, Lon: gps.Lon})
	if err != nil || !out.Found {
		return ""
	}

	parts := []string{fmt.Sprintf("最近的站牌「%s」", out.Name)}
	for i, b := range out.Buses {
		if i >= 3 {
			break
		}
		bus := fmt.Sprintf("%s路", b.Route)
		if b.Direction != "" {
			bus = fmt.Sprintf("%s路（%s）", b.Route, b.Direction)
		}
		if b.HasETA {
			parts = append(parts, fmt.Sprintf("%s還有%d分鐘", bus, b.ETAMinutes))
		} else {
			parts = append(parts, fmt.Sprintf("%s無到站時間", bus))
		}
	}
	return strings.Join(parts, "，")
}
