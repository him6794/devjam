package skill

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"google.golang.org/genai"
)

type VisionReadSignIn struct {
	ImageData []byte
	MIMEType  string
}

type VisionReadSignOut struct {
	Found           bool   `json:"found"`
	StopName        string `json:"stop_name"`
	DestinationText string `json:"destination_text"`
}






const visionPrompt = `這是一張照片，可能拍到台灣的公車站牌。
如果照片中清楚可見公車站牌，請提取：
1. stop_name：站牌上的站名（繁體中文，如「捷運公館站」）
2. destination_text：任一路線旁「往ＸＸＸ」的目的地文字（繁體中文，若有多個取最清楚的一個）
如果照片中沒有清楚可辨識的公車站牌文字，請將 found 設為 false，其餘欄位留空字串。`

var visionResponseSchema = &genai.Schema{
	Type: genai.TypeObject,
	Properties: map[string]*genai.Schema{
		"found":            {Type: genai.TypeBoolean},
		"stop_name":        {Type: genai.TypeString},
		"destination_text": {Type: genai.TypeString},
	},
	Required: []string{"found", "stop_name", "destination_text"},
}




type VisionReadSign struct {
	client *genai.Client
	model  string
}

func NewVisionReadSign(client *genai.Client) *VisionReadSign {
	return &VisionReadSign{client: client, model: "gemini-2.5-flash"}
}

func (s *VisionReadSign) Name() string { return "vision_read_sign" }

func (s *VisionReadSign) Description() string {
	return "Read a photo of a Taiwanese bus stop sign and extract the station name and any visible destination (往XXX) text."
}

func (s *VisionReadSign) InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"image_base64": map[string]any{"type": "string", "description": "base64-encoded photo"},
			"mime_type":    map[string]any{"type": "string", "description": "e.g. image/jpeg", "default": "image/jpeg"},
		},
		"required": []string{"image_base64"},
	}
}

func (s *VisionReadSign) Do(ctx context.Context, in VisionReadSignIn) (VisionReadSignOut, error) {
	mime := in.MIMEType
	if mime == "" {
		mime = "image/jpeg"
	}
	resp, err := s.client.Models.GenerateContent(ctx, s.model,
		[]*genai.Content{{
			Role: "user",
			Parts: []*genai.Part{
				{Text: visionPrompt},
				{InlineData: &genai.Blob{MIMEType: mime, Data: in.ImageData}},
			},
		}},
		&genai.GenerateContentConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   visionResponseSchema,
		},
	)
	if err != nil {
		return VisionReadSignOut{}, fmt.Errorf("vision_read_sign: generate: %w", err)
	}
	var out VisionReadSignOut
	if err := json.Unmarshal([]byte(resp.Text()), &out); err != nil {
		return VisionReadSignOut{}, fmt.Errorf("vision_read_sign: decode model response: %w", err)
	}
	return out, nil
}

func (s *VisionReadSign) Invoke(ctx context.Context, raw json.RawMessage) (any, error) {
	var args struct {
		ImageBase64 string `json:"image_base64"`
		MIMEType    string `json:"mime_type"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, fmt.Errorf("skill %s: bad args: %w", s.Name(), err)
	}
	data, err := base64.StdEncoding.DecodeString(args.ImageBase64)
	if err != nil {
		return nil, fmt.Errorf("skill %s: bad image_base64: %w", s.Name(), err)
	}
	return s.Do(ctx, VisionReadSignIn{ImageData: data, MIMEType: args.MIMEType})
}
