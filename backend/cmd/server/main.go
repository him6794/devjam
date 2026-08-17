// Command server runs the accessibility bus-assistant API described in
// api.md. It expects a station index built by cmd/indexer to already exist
// on disk (see STOPS_INDEX_PATH below).
package main

import (
	"context"
	"log"
	"os"

	speech "cloud.google.com/go/speech/apiv2"
	"cloud.google.com/go/storage"
	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"google.golang.org/genai"

	"devjam-backend/internal/agent"
	"devjam-backend/internal/httpapi"
	"devjam-backend/internal/pda"
	"devjam-backend/internal/profile"
	"devjam-backend/internal/skill"
	"devjam-backend/internal/stopindex"
)

func main() {
	stopsPath := envOr("STOPS_INDEX_PATH", "data/stops.json")
	index, err := stopindex.Load(stopsPath)
	if err != nil {
		log.Fatalf("server: failed to load station index from %s (run cmd/indexer first): %v", stopsPath, err)
	}
	log.Printf("server: loaded %d stations from %s", index.Len(), stopsPath)

	pdaClient := pda.NewClient()
	profiles := profile.NewStore()

	nearestStops := skill.NewNearestStops(index)
	stopETA := skill.NewStopETA(pdaClient, index)

	registry := skill.NewRegistry()
	registry.Register(nearestStops)
	registry.Register(stopETA)

	// Vision and route extraction share one Gemini client. It is optional:
	// if Vertex AI credentials/project aren't configured, log it and keep
	// serving GPS-only analyze rather than refusing to start. See
	// agent.VisionAgent / httpapi.pickStation for how analyze degrades when
	// this is nil; route_extract's regex fallback keeps the numeric-route
	// voice path working without it.
	var (
		visionSign   *skill.VisionReadSign
		routeExtract *skill.RouteExtract
	)
	genaiClient, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		Project:  envOr("GCP_PROJECT", "devjam26aug17tpe-1280"),
		Location: envOr("GCP_LOCATION", "us-central1"),
		Backend:  genai.BackendVertexAI,
	})
	if err != nil {
		log.Printf("server: Vertex AI client unavailable, vision-based disambiguation and spoken-numeral route extraction disabled: %v", err)
	} else {
		visionSign = skill.NewVisionReadSign(genaiClient)
		registry.Register(visionSign)
		log.Printf("server: vision_read_sign enabled via Vertex AI Gemini")
	}
	routeExtract = skill.NewRouteExtract(genaiClient) // nil client → regex-only fallback
	registry.Register(routeExtract)

	// Navigate works without genaiClient too (exact/substring station-name
	// match only, no fuzzy resolution) — see skill.Navigate.resolveDestination.
	navigate := skill.NewNavigate(index, genaiClient)
	registry.Register(navigate)

	// LiveGuide needs its own Vertex AI client pinned to "global": verified
	// directly against this project (client.Live.Connect against
	// gemini-live-2.5-flash) that us-central1/us-east4/europe-west4 all
	// reject the Live/BidiGenerateContent websocket handshake with "model
	// was not found" for this publisher model, while global connects
	// successfully — same region quirk as Speech-to-Text v2's synchronous
	// Recognize above, just for a different API family.
	var liveGuide *skill.LiveGuide
	liveClient, liveErr := genai.NewClient(context.Background(), &genai.ClientConfig{
		Project:  envOr("GCP_PROJECT", "devjam26aug17tpe-1280"),
		Location: "global",
		Backend:  genai.BackendVertexAI,
	})
	if liveErr != nil {
		log.Printf("server: Vertex AI Live client unavailable, live_guide disabled: %v", liveErr)
		liveGuide = skill.NewLiveGuide(nil)
	} else {
		liveGuide = skill.NewLiveGuide(liveClient)
		log.Printf("server: live_guide enabled via Vertex AI Gemini Live (global)")
	}

	// TTS is likewise optional: without it, voice_audio_url in the response
	// just comes back as "" instead of a playable URL (see
	// httpapi.AnalyzeHandler.synthesizeVoiceAudioURL).
	var ttsSkill *skill.TTS
	speechClient, speechErr := texttospeech.NewClient(context.Background())
	gcsClient, gcsErr := storage.NewClient(context.Background())
	if speechErr != nil || gcsErr != nil {
		log.Printf("server: Text-to-Speech/Storage client unavailable, voice_summary audio disabled: tts=%v storage=%v", speechErr, gcsErr)
	} else {
		ttsSkill = skill.NewTTS(speechClient, gcsClient, envOr("TTS_BUCKET", "devjam26aug17tpe-1280-tts-audio"))
		registry.Register(ttsSkill)
		log.Printf("server: tts enabled via Cloud Text-to-Speech")
	}

	// STT is optional like TTS: without it, /api/voice_route still answers
	// text requests, and answers audio requests with 503 stt_unavailable so
	// the frontend falls back to asking the rider to type the route (see
	// httpapi.VoiceRouteHandler).
	var sttSkill *skill.SpeechToText
	sttClient, sttErr := speech.NewClient(context.Background())
	if sttErr != nil {
		log.Printf("server: Speech-to-Text client unavailable, voice_route audio disabled: %v", sttErr)
	} else {
		// STT v2 的同步 Recognize 只存在 global location（區域型 recognizer
		// 僅供 streaming/batch），所以不能用 Vertex AI 的 GCP_LOCATION。
		sttSkill = skill.NewSpeechToText(sttClient, envOr("GCP_PROJECT", "devjam26aug17tpe-1280"), envOr("STT_LOCATION", "global"), envOr("STT_MODEL", "latest_long"))
		registry.Register(sttSkill)
		log.Printf("server: speech_to_text enabled via Cloud Speech-to-Text")
	}

	server := &httpapi.Server{
		Analyze:    httpapi.NewAnalyzeHandler(agent.NewOrchestrator(), nearestStops, stopETA, visionSign, ttsSkill, profiles),
		Profile:    httpapi.NewProfileHandler(profiles),
		VoiceRoute: httpapi.NewVoiceRouteHandler(sttSkill, routeExtract),
		Navigate:   httpapi.NewNavigateHandler(navigate),
		LiveGuide:  httpapi.NewLiveGuideHandler(liveGuide),
		Registry:   registry,
	}

	r := httpapi.NewRouter(server)
	addr := ":" + envOr("PORT", "8080")
	log.Printf("server: listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
