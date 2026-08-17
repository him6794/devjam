1. POST /api/analyze
前端會給：使用者gps位置等資訊、當前影格擷取的圖片、可選的想要路線
- JSON 或 multipart/form-data（multipart 時 image 為檔案，其餘欄位為表單值）
- 可選欄位 wanted_route：使用者想搭的路線編號（見 §5），如「307」；未填則行為與以往相同
後端回：{
  "status": "not_found",
  "message": "尚未偵測到站牌"
}
或辨識成功時 {
  "status": "success",
  "station_name": "新益里",
  "wanted_route": "307",
  "wanted_route_found": true,
  "buses": [
    {
      "route": "307",
      "eta_minutes": 3,
      "direction": "往台北車站",
      "urgency": "high",
      "is_wanted": true
    },
    {
      "route": "202",
      "eta_minutes": 8,
      "direction": "往公館",
      "urgency": "low",
      "is_wanted": false
    }
  ],
  "display": {
    "safe_zone_position": "top",
    "font_scale": 1.5
  },
  "voice_summary": "你要搭的307路還有3分鐘進站，往台北車站",
  "voice_audio_url": "https://storage.googleapis.com/.../xxx.mp3"
}
wanted_route 有設定時：相符的路線排最前面且 is_wanted=true，其餘路線 urgency 上限為 low；
voice_summary 也會先說「你要搭的ＯＯ路」。此站沒有該路線時 wanted_route_found=false，
voice_summary 改說「此站牌沒有ＯＯ路，最近的是ＸＸ路…」。


2. GET /api/profile 與 POST /api/profile(使用者偏好)
前端會給：使用者ID(header之類)
後端回傳：{
  "exists": true,
  "impairment_type": "tunnel_vision",
  "safe_zone": { "x": 50, "y": 20, "radius": 30 },
  "font_scale": 1.5,
  "voice_enabled": true
}


3. POST /api/notify_driver
前端會給類似：{
  "route": "307",
  "station_name": "捷運公館站",
  "impairment_type": "tunnel_vision"
}
後端回傳：{
  "status": "success",
  "alert_id": "alert-1"
}


4. GET /api/driver_alerts?route=307
司機端用路線查詢最近 10 分鐘內的乘客告警。後端回傳：{
  "alerts": [
    {
      "alert_id": "alert-1",
      "route": "307",
      "station_name": "捷運公館站",
      "impairment_type": "tunnel_vision",
      "timestamp": "2026-08-17T08:43:31Z",
      "acknowledged": false
    }
  ]
}

告警與個人偏好目前由 API process 記憶體保存；重啟容器後會清空。


5. POST /api/voice_route（語音路線選擇，Journey Agent v1）
前端會給其一（有 text 就優先走文字，否則走語音辨識）：
- 語音：{ "audio_base64": "...", "audio_mime": "audio/webm" }
  （MediaRecorder 錄下的 webm/opus；須設定 Cloud Speech-to-Text，未設定時回 503 stt_unavailable）
- 文字：{ "text": "我要搭307路" }
後端回：{
  "status": "success",
  "route": "307",
  "transcript": "我要搭307路"
}
或聽不出路線時：{
  "status": "no_route",
  "message": "沒有聽到路線號碼，請說出例如「307」。"
}
路線編號由 Gemini 從轉錄文字擷取（可處理中文數字「三零七」→「307」）；
Gemini 不可用時退回到純數字 regex，數字路線仍可完整運作。
