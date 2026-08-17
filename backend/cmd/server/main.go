// Command server runs the accessibility bus-assistant API described in
// api.md. It expects a station index built by cmd/indexer to already exist
// on disk (see STOPS_INDEX_PATH below).
package main

import (
	"context"
	"log"
	"os"

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

	// Vision is optional: if Vertex AI credentials/project aren't
	// configured, log it and keep serving GPS-only analyze rather than
	// refusing to start. See agent.VisionAgent / httpapi.pickStation for
	// how analyze degrades when this is nil.
	var visionSign *skill.VisionReadSign
	genaiClient, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		Project:  envOr("GCP_PROJECT", "devjam26aug17tpe-1280"),
		Location: envOr("GCP_LOCATION", "us-central1"),
		Backend:  genai.BackendVertexAI,
	})
	if err != nil {
		log.Printf("server: Vertex AI client unavailable, vision-based disambiguation disabled: %v", err)
	} else {
		visionSign = skill.NewVisionReadSign(genaiClient)
		registry.Register(visionSign)
		log.Printf("server: vision_read_sign enabled via Vertex AI Gemini")
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

	server := &httpapi.Server{
		Analyze:  httpapi.NewAnalyzeHandler(agent.NewOrchestrator(), nearestStops, stopETA, visionSign, ttsSkill, profiles),
		Profile:  httpapi.NewProfileHandler(profiles),
		Registry: registry,
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
