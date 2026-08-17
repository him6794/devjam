1. POST /api/analyze
前端會給：使用者gps位置等資訊、當前影格擷取的圖片
後端回：{
  "status": "not_found",
  "message": "尚未偵測到站牌"
}
或辨識成功時 {
  "status": "success",
  "station_name": "新益里",
  "buses": [
    {
      "route": "307",
      "eta_minutes": 3,
      "direction": "往台北車站",
      "urgency": "high"
    },
    {
      "route": "202",
      "eta_minutes": 8,
      "direction": "往公館",
      "urgency": "medium"
    }
  ],
  "display": {
    "safe_zone_position": "top",
    "font_scale": 1.5
  },
  "voice_summary": "307路還有3分鐘進站，往台北車站",
  "voice_audio_url": "https://storage.googleapis.com/.../xxx.mp3"
}


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
