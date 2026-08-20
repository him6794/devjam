package skill

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	speech "cloud.google.com/go/speech/apiv2"
	"cloud.google.com/go/speech/apiv2/speechpb"
)

type STTIn struct {
	Audio    []byte
	MIMEType string
}

type STTOut struct {
	Transcript string `json:"transcript"`
}







type SpeechToText struct {
	client   *speech.Client
	project  string
	location string
	model    string
}

func NewSpeechToText(client *speech.Client, project, location, model string) *SpeechToText {
	return &SpeechToText{client: client, project: project, location: location, model: model}
}

func (s *SpeechToText) Name() string { return "speech_to_text" }

func (s *SpeechToText) Description() string {
	return "Transcribe a short spoken Traditional Chinese audio clip into text."
}

func (s *SpeechToText) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"audio_base64": map[string]any{"type": "string", "description": "base64-encoded audio"},
			"mime_type":    map[string]any{"type": "string", "description": "e.g. audio/webm", "default": "audio/webm"},
		},
		"required": []string{"audio_base64"},
	}
}

func (s *SpeechToText) Do(ctx context.Context, in STTIn) (STTOut, error) {
	if s.client == nil {
		return STTOut{}, errors.New("speech_to_text: client not configured")
	}
	if len(in.Audio) == 0 {
		return STTOut{}, errors.New("speech_to_text: empty audio")
	}

	location := s.location
	if location == "" {
		location = "global"
	}
	
	
	
	resp, err := s.client.Recognize(ctx, &speechpb.RecognizeRequest{
		Recognizer: fmt.Sprintf("projects/%s/locations/%s/recognizers/_", s.project, location),
		Config: &speechpb.RecognitionConfig{
			DecodingConfig: &speechpb.RecognitionConfig_AutoDecodingConfig{
				AutoDecodingConfig: &speechpb.AutoDetectDecodingConfig{},
			},
			
			
			LanguageCodes: []string{"cmn-Hant-TW"},
			Model:         s.model,
		},
		AudioSource: &speechpb.RecognizeRequest_Content{Content: in.Audio},
	})
	if err != nil {
		return STTOut{}, fmt.Errorf("speech_to_text: recognize: %w", err)
	}
	for _, result := range resp.Results {
		for _, alt := range result.Alternatives {
			if alt.Transcript != "" {
				return STTOut{Transcript: alt.Transcript}, nil
			}
		}
	}
	return STTOut{}, errors.New("speech_to_text: no transcript in response")
}

func (s *SpeechToText) Invoke(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		AudioBase64 string `json:"audio_base64"`
		MIMEType    string `json:"mime_type"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("skill %s: bad args: %w", s.Name(), err)
	}
	data, err := base64.StdEncoding.DecodeString(args.AudioBase64)
	if err != nil {
		return nil, fmt.Errorf("skill %s: bad audio_base64: %w", s.Name(), err)
	}
	return s.Do(ctx, STTIn{Audio: data, MIMEType: args.MIMEType})
}
