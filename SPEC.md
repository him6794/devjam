# 城市之眼 — 智慧城市無障礙公車資訊 Agent

## 一、題目定位

- **主題**：Agent - 智慧城市
- **TA**：視覺障礙族群 — 隧道視野、中心黑點（視野缺損）、全盲
- **痛點**：搭公車時看不清/看不到公車號碼與到站時間（站牌資訊）
- **載具**：手機 Web（瀏覽器，不做原生 App）
- **比賽限制**：只能用 Google 生態（Vertex AI / Gemini），不可用 AWS Bedrock、OpenAI、Anthropic 等競品

## 二、核心功能（三個模組）

### 模組 1：拍照辨識 + 個人化呈現（乘客端）

1. 使用者拍下公車站牌照片
2. 後端呼叫 **Vertex AI Gemini** 多模態辨識，取出公車號碼、到站時間、摘要
3. 依照使用者的障礙類型客製化呈現：
   - **不是**原地 AR 覆蓋（像 Google 翻譯那樣貼在原文字位置）
   - **是**把資訊重新繪製到使用者「看得到的安全區」（例如中心黑點的人，資訊要放在畫面上緣而不是正中央）
   - 全盲使用者：跳過畫面，純語音播報（**Cloud Text-to-Speech**）

### 模組 2：使用者偏好校準（只做一次）

1. 第一次使用時，選擇障礙類型 + 簡單校準安全顯示區 + 字體大小/語音開關
2. 存進 **Firestore**（`users` collection）
3. 之後每次使用，後端自動讀取這個人的偏好，不用重選

### 模組 3：通知司機（扣合智慧城市，雙受眾設計）

1. 乘客在站牌拍照辨識出公車號碼後，可按「通知司機」
2. 後端把警示寫進 **Firestore**（`bus_alerts` collection：路線、站名、障礙類型、時間）
3. 司機端頁面（另一個網頁）選定路線後，**每 3 秒輪詢**後端 API，有新警示就跳出提示「XX路線有視障乘客在等車」

## 三、技術架構

| 功能 | 技術 |
|---|---|
| 圖像辨識＋摘要 | Vertex AI Gemini 2.5 Flash（多模態） |
| 語音輸出 | Google Cloud Text-to-Speech |
| 資料庫（使用者偏好、司機警示） | Firestore |
| 後端 | Python Flask |
| 前端 | 純 HTML/JS 手機網頁，`localStorage` 存裝置 `user_id`（不做登入系統） |
| 部署（加分項，時間夠再做） | Cloud Run |

### API 一覽

- `POST /api/analyze` — 上傳照片，回傳辨識摘要＋依偏好客製化的呈現方式
- `GET /api/profile` / `POST /api/profile` — 讀寫使用者偏好
- `POST /api/notify_driver` — 乘客通知司機
- `GET /api/driver_alerts?route=X` — 司機端查詢該路線的警示

檔案位置：`hackathon_smartcity/`（`pipeline.py` 核心 AI 流程、`firestore_client.py` 資料庫存取、`server.py` Flask API + 前端頁面）

## 四、MVP 優先順序

**必做（demo 靈魂，不能開天窗）**
1. 拍照 → Gemini 辨識 → TTS 語音（模組1核心）
2. Firestore 存使用者偏好，自動讀取（模組2）

**應該做（讓評審覺得是系統不是玩具）**
3. 通知司機功能（模組3）＋司機端頁面
4. 依偏好客製化畫面呈現（安全區、字體）

**有餘力再做**
5. Cloud Run 部署，拿到公開網址
6. Gemini function calling（讓「查 Firestore」「選呈現模式」變成 Agent 自主決策，而非寫死邏輯）—— 呼應賽事名稱「Agent」

**先砍掉**
- 即時逐幀 AR 影像疊字（風險太高，用「拍照→重新繪製」取代）
- 模型微調、群眾回報防濫用機制、完整登入系統

## 五、分工建議（依實際人數調整）

| 角色 | 負責內容 | 對應檔案 |
|---|---|---|
| **A．AI Pipeline** | Vertex AI Gemini 串接、圖片辨識 prompt 設計、TTS 串接 | `pipeline.py` |
| **B．後端＋資料庫** | Firestore schema、API 開發（profile / notify_driver / driver_alerts）、Flask 路由 | `firestore_client.py`、`server.py` |
| **C．前端** | 乘客端頁面（拍照、校準 UI、結果呈現、通知司機按鈕）、司機端頁面（路線選擇、警示輪詢顯示） | `server.py` 內的 HTML/JS |
| **D．簡報＋整合測試**（若人力夠） | 簡報製作、demo 腳本、錄影、GCP 部署、整合各模組測試 | — |

## 六、待確認事項（現在就要處理，不然會卡住後面所有人）

- [ ] 新 GCP 專案 ID（目前測到原專案 billing 未啟用，需換成主辦方提供的專案）
- [ ] 驗證方式：service account 金鑰檔 or `gcloud auth application-default login`
- [ ] Gemini / Text-to-Speech / Firestore 三個 API 是否已在新專案啟用
