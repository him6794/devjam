package skill

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"cloud.google.com/go/storage"
	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
)

type TTSIn struct {
	Text string `json:"text"`
}

type TTSOut struct {
	AudioURL string `json:"audio_url"`
}

// TTS synthesizes voice_summary text into an MP3 via Cloud Text-to-Speech
// and serves it back as a public Cloud Storage URL — api.md's
// voice_summary field is a file, not text, so the audio itself is the
// contract, not a summary of it.
//
// Objects are named by the SHA-256 of their text, so /api/analyze's
// repeated polling (the same "3 分鐘進站" sentence, requested every 1-2s
// while a rider waits) reuses one upload instead of re-synthesizing and
// re-uploading identical audio on every call — checked both in-process
// (cache) and against the bucket itself (objectExists), so the saving
// holds across server restarts too.
type TTS struct {
	speech *texttospeech.Client
	gcs    *storage.Client
	bucket string
	voice  string

	mu    sync.Mutex
	cache map[string]string // text -> public URL
}

func NewTTS(speech *texttospeech.Client, gcs *storage.Client, bucket string) *TTS {
	return &TTS{
		speech: speech,
		gcs:    gcs,
		bucket: bucket,
		voice:  "cmn-TW-Wavenet-A",
		cache:  map[string]string{},
	}
}

func (s *TTS) Name() string { return "tts" }

func (s *TTS) Description() string {
	return "Synthesize Traditional Chinese text into speech and return a public URL to the MP3."
}

func (s *TTS) InputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"text": map[string]any{"type": "string"}},
		"required":   []string{"text"},
	}
}

func (s *TTS) Do(ctx context.Context, in TTSIn) (TTSOut, error) {
	if in.Text == "" {
		return TTSOut{}, errors.New("tts: empty text")
	}

	s.mu.Lock()
	if url, ok := s.cache[in.Text]; ok {
		s.mu.Unlock()
		return TTSOut{AudioURL: url}, nil
	}
	s.mu.Unlock()

	objectName := fmt.Sprintf("tts/%x.mp3", sha256.Sum256([]byte(in.Text)))
	obj := s.gcs.Bucket(s.bucket).Object(objectName)
	url := fmt.Sprintf("https://storage.googleapis.com/%s/%s", s.bucket, objectName)

	if exists, err := s.objectExists(ctx, obj); err == nil && exists {
		s.mu.Lock()
		s.cache[in.Text] = url
		s.mu.Unlock()
		return TTSOut{AudioURL: url}, nil
	}

	resp, err := s.speech.SynthesizeSpeech(ctx, &texttospeechpb.SynthesizeSpeechRequest{
		Input: &texttospeechpb.SynthesisInput{
			InputSource: &texttospeechpb.SynthesisInput_Text{Text: in.Text},
		},
		Voice: &texttospeechpb.VoiceSelectionParams{
			LanguageCode: "cmn-TW",
			Name:         s.voice,
		},
		AudioConfig: &texttospeechpb.AudioConfig{
			AudioEncoding: texttospeechpb.AudioEncoding_MP3,
		},
	})
	if err != nil {
		return TTSOut{}, fmt.Errorf("tts: synthesize: %w", err)
	}

	w := obj.NewWriter(ctx)
	w.ContentType = "audio/mpeg"
	if _, err := w.Write(resp.AudioContent); err != nil {
		w.Close()
		return TTSOut{}, fmt.Errorf("tts: write %s: %w", objectName, err)
	}
	if err := w.Close(); err != nil {
		return TTSOut{}, fmt.Errorf("tts: close %s: %w", objectName, err)
	}

	s.mu.Lock()
	s.cache[in.Text] = url
	s.mu.Unlock()
	return TTSOut{AudioURL: url}, nil
}

func (s *TTS) objectExists(ctx context.Context, obj *storage.ObjectHandle) (bool, error) {
	_, err := obj.Attrs(ctx)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, storage.ErrObjectNotExist) {
		return false, nil
	}
	return false, err
}

func (s *TTS) Invoke(ctx context.Context, raw json.RawMessage) (any, error) {
	var in TTSIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("skill %s: bad args: %w", s.Name(), err)
	}
	return s.Do(ctx, in)
}
