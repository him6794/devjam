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


6. POST /api/navigate（導航規劃：給地點 → 規劃路線 → 逐步引導）
前端會給：{
  "lat": 25.062218,
  "lng": 121.566257,
  "destination": "捷運西門站"
}
destination 是自由文字（口語或打字皆可），先用站名子字串比對，比對不到時（且 Gemini
已設定）才用 Gemini 從已知站名清單中選出最符合的一個——所以永遠不會回傳資料庫中不存在
的站。

後端回（找到直達路線）：{
  "found": true,
  "origin_station_name": "新益里",
  "destination_station_name": "捷運西門站",
  "route": "307",
  "direction": "往板橋",
  "steps": [
    { "kind": "walk", "text": "請走到「新益里」站牌" },
    { "kind": "board", "text": "上車前確認車頭或車身顯示「307」，方向為「往板橋」" },
    { "kind": "ride", "text": "搭乘307路，往板橋方向" },
    { "kind": "alight", "text": "到「捷運西門站」站下車" }
  ]
}
或找不到目的地/沒有直達路線時：{
  "found": false,
  "message": "目前資料庫中沒有直達「新益里」到「捷運西門站」的路線，可能需要轉乘，建議到站後詢問站務或司機"
}

v1 只處理「同一條路線直達」的情況，不做多段轉乘規劃（沒有 Google Routes/Places API
金鑰，這是用專案既有的 stopindex + pda5284 資料做的務實版本）；需要轉乘時明確告知使用者，
不會給一個無法驗證的假路線。


7. GET /api/live_guide（WebSocket，Gemini Live 即時視覺協助）
相機一開就連線，跟相機同壽命（離開相機頁/關閉分頁會一起關掉）。這不是「後端先判斷
有沒有危險再通知 Gemini」，是 Gemini Live 自己持續看鏡頭畫面、自己決定要不要開口——
危險提示、公車進站、車門位置，全部由模型主動判斷，不是被動等外部事件告知。

協定（純 binary/text WebSocket message，沒有 JSON 包裝）：
- 前端 → 後端：raw JPEG bytes，每 1-2 秒一張（見 passenger.js sendLiveGuideFrame）
- 後端 → 前端：UTF-8 文字，一句要唸的話——只有模型判斷「這個畫面值得講」時才會收到，
  多數時候完全不會收到訊息

實作備註（皆為實測驗證，非文件推測）：
- Vertex AI 的 Live API 對這個專案只有 location=global 可用（us-central1 / us-east4 /
  europe-west4 皆回 404 "model not found"，与 STT v2 的 global-only 限制同樣的區域
  限制模式），model 為 gemini-live-2.5-flash（GA，非 preview）
- 純視覺 frame（SendRealtimeInput 的 video blob）不會自己觸發模型完成一輪回合——
  Live API 的回合機制是為語音對話設計的語音活動偵測，video 只是背景上下文。第一版
  「送一張 frame 就等回覆」的寫法會讓 PushFrame 永久阻塞（用一個會 timeout 的
  goroutine 測試才抓到，光靠瀏覽器端「frame 送出去了」看不出這個問題）。修法：frame
  持續送當背景上下文（不等回覆），後端另外每 4 秒（liveGuideJudgmentInterval）用一句
  文字提示「請判斷目前畫面」主動觸發一輪真正的回合
- 讀取 frame 與請求判斷分成兩個獨立 goroutine：讀取迴圈只管收 frame、立即
  PushFrame（不阻塞），判斷迴圈在自己的計時器上呼叫 RequestJudgment。兩者共用一個
  「任一方結束就關閉整條連線」的機制，這樣使用者離開相機頁時，即使判斷迴圈正在等
  Gemini 回覆，也會被立刻中斷，不會延遲關閉
- nginx 需要額外轉發 Upgrade/Connection header 才能讓 WebSocket 交握通過反向代理
  （見 nginx.conf 的 map $http_upgrade $connection_upgrade），一般 HTTP proxy_pass
  設定預設不會轉發這兩個 header
