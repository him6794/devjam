# 城市之眼（ReVision City）— 技術架構文檔

> 無障礙公車助手系統的技術架構，內容對應實際部署的系統，不是規劃階段的構想

**版本**：2.0
**最後更新**：2026-08-18

---

## 目錄

1. [系統概覽](#系統概覽)
2. [整體架構](#整體架構)
3. [前端架構](#前端架構)
4. [後端架構](#後端架構)
5. [API 設計](#api-設計)
6. [數據流](#數據流)
7. [部署架構](#部署架構)
8. [核心組件詳解](#核心組件詳解)
9. [已知的技術債 / 尚未實作](#已知的技術債--尚未實作)

---

## 系統概覽

### 產品定位

城市之眼（ReVision City）是一個針對身心障礙人士（特別是隧道視野、中心視野缺損患者）設計的無障礙公車助手，結合視覺辨識、GPS 定位、AI 語音合成與 Gemini Live 即時視覺協助，幫助使用者快速識別當前站位、獲得即時到站資訊，並在候車與上車過程中持續獲得口語提示。

### 核心功能

| 功能 | 說明 | 角色 | 端點 |
|-----|------|------|------|
| **站牌視覺辨識 + ETA** | 拍照或持續 GPS，辨識站牌並回傳當前站位所有路線到站時間 | 乘客 | `POST /api/analyze` |
| **語音選路線** | 講出或打字說出想搭的路線號，供後續分析優先標記 | 乘客 | `POST /api/voice_route` |
| **導航規劃** | 給目的地文字，找出同站直達路線與逐步引導文字 | 乘客 | `POST /api/navigate` |
| **Live Guide** | 相機持續開啟期間，Gemini Live 主動觀看畫面並在判斷有必要時開口提示（公車進站、車門位置、危險等） | 乘客 | `GET /api/live_guide`（WebSocket） |
| **個人偏好** | 校準安全視野窗位置、字型縮放、語音開關 | 乘客 | `GET/POST /api/profile` |
| **司機告警** | 乘客通知駕駛需要協助，司機端輪詢查看 | 司機 | `POST /api/notify_driver` / `GET /api/driver_alerts` |

### 技術棧

- **前端**：伺服端渲染 HTML（Jinja2）+ Vanilla JavaScript + CSS3，Flask 負責模板與靜態檔案伺服
- **網關層**：Nginx，TLS 終止 + 反向代理 + WebSocket Upgrade 轉發
- **後端**：Go（Gin 框架），Agent/Skill 架構（見〈後端架構〉）
- **AI 服務**：Google Vertex AI —— Gemini（視覺辨識、文字推理）、Gemini Live（即時視覺協助）、Cloud Text-to-Speech、Cloud Speech-to-Text v2
- **數據源**：台北市公車動態系統 pda5284（`https://pda5284.gov.taipei/MQS/`，公開、無金鑰、自我限速 ~2 req/s）
- **狀態存儲**：目前皆為**行程記憶體內狀態**，無資料庫（見〈已知的技術債〉）
- **基礎設施**：單一 GCE VM，Docker Compose 跑三個容器（gateway / frontend / api），Let's Encrypt 憑證由主機上的 certbot 直接管理

---

## 整體架構

```
                          瀏覽器（乘客端 / 司機端）
                                   │
                                   │ HTTPS / WSS
                                   ▼
                    ┌───────────────────────────┐
                    │   Nginx Gateway (容器)     │
                    │   - TLS 終止（certbot 憑證）│
                    │   - /api/*  → api:8080     │
                    │   - /*      → frontend:5000│
                    │   - WS Upgrade 轉發         │
                    └──────────┬─────────┬───────┘
                               │         │
                 ┌─────────────┘         └─────────────┐
                 ▼                                      ▼
    ┌─────────────────────────┐          ┌───────────────────────────┐
    │  frontend (Flask 容器)   │          │   api (Go/Gin 容器)        │
    │  - templates/*.html     │          │   - HTTP handlers          │
    │  - static/js, static/css│          │   - Agent 編排 (Blackboard) │
    │  - 純伺服模板 + 靜態檔   │          │   - Skill Registry          │
    └─────────────────────────┘          │   - live_guide WS session   │
                                          └──────┬──────────────┬──────┘
                                                 │              │
                          ┌──────────────────────┤              ├────────────────────┐
                          ▼                      ▼              ▼                    ▼
              ┌────────────────────┐  ┌─────────────────┐  ┌───────────────┐  ┌──────────────┐
              │  pda5284.gov.taipei│  │ Vertex AI Gemini │  │ Cloud TTS/STT │  │ Cloud Storage │
              │  （公車即時 ETA / │  │ (Vision / LLM /  │  │ (語音合成/辨識)│  │ (語音 mp3 快取)│
              │   GPS，公開 API）  │  │  Live API)        │  │               │  │               │
              └────────────────────┘  └─────────────────┘  └───────────────┘  └──────────────┘
```

沒有 Firestore、Redis、Cloud Run、Cloud Tasks/Scheduler、Google Routes API —— 這些出現在早期規劃文件（`plan.md`）與本文件舊版中，但實際沒有被建置（見〈已知的技術債〉）。

---

## 前端架構

### 頁面

| 路徑 | 模板 | 對應 JS | 用途 |
|------|------|---------|------|
| `/`（乘客端） | `templates/passenger.html` | `static/js/passenger.js` | 相機/GPS 校準精靈 → 拍照分析 → 結果顯示 → Live Guide |
| `/driver` | `templates/driver.html` | `static/js/driver.js` | 路線查詢 + 告警列表 |
| — | `templates/base.html` | `static/js/common.js` | 共用版型、共用工具函式 |
| — | — | `static/js/manual-upload.js` | 相機不可用時的手動上傳備援路徑 |
| — | — | `static/js/mock-data.js` | 離線/展示用的假資料 |

乘客端是多畫面（screen）流程：先做一次視覺障礙類型校準精靈（隧道視野／中心缺損／其他 → 點選安全視野範圍），再進入相機主畫面。frontend 容器本身不含業務邏輯，純粹渲染模板與伺服靜態檔，所有資料都是瀏覽器 JS 直接呼叫 `/api/*`（同源，經 gateway 轉發到 api 容器）。

### 設計系統（`static/css/main.css`，已與程式碼核對）

| 用途 | Token | 值 |
|-----|-------|-----|
| 背景 | `--color-bg` | `#F7F5F0` |
| 卡片 | `--color-surface` | `#FFFFFF` |
| 正文 | `--color-text` | `#14181F` |
| 主色（CTA / 安全視野高亮） | `--color-primary` | `#C8850C` |
| 次色 | `--color-secondary` | `#1D5FB8` |
| 錯誤 | `--color-danger` | `#C62828` |
| 成功 | `--color-success` | `#1E8E3E` |

- 字體棧：`-apple-system, BlinkMacSystemFont, "Noto Sans TC", "PingFang TC", sans-serif`
- 全域 `--font-scale` 由使用者校準結果動態覆寫，所有字級都用 `calc(... * var(--font-scale))` 推導
- 觸控目標最小 48px（`--touch-min`），間距以 8px 為基準

---

## 後端架構（`backend/`，Go / Gin）

### 分層

```
cmd/server          進入點：讀環境變數、組出所有 skill/agent/handler、啟動 Gin
cmd/indexer          離線批次工具：產生 internal/stopindex 用的 data/stops.json

internal/httpapi     Gin handler 層，一個檔案對一個端點（見 router.go）
internal/agent        Blackboard + Wave 編排器（見下）
internal/skill        可被 agent 呼叫、也可被 LLM function-calling 呼叫的最小業務單元
internal/pda           pda5284.gov.taipei 的 HTTP client（自我限速）
internal/stopindex     站位空間索引（記憶體內，啟動時載入一次）
internal/profile       使用者無障礙偏好（記憶體內 store）
```

### Agent 編排（`internal/agent`）

`POST /api/analyze` 背後是一個**同步、單次請求內完成**的 Blackboard 模式編排（不是背景佇列，沒有 Cloud Tasks）：

```go
type Blackboard struct { /* Lat, Lon, ImageData, Candidates, VisionFound, Buses, ... */ }

type Agent interface {
    Name() string
    Run(ctx context.Context, bb Blackboard) Contribution
}
```

- **Wave 1**（平行執行，互不依賴）：`GeoAgent`（GPS → 半徑內候選站位，純記憶體計算）／`VisionAgent`（有照片才跑，呼叫 Gemini 讀站牌文字）／`ProfileAgent`（讀使用者偏好）
- Wave 1 結束後，`httpapi` 層依 Vision 結果從 Geo 的候選站位中鎖定一個（`pickStation`）
- **Wave 2**：`TransitAgent`（鎖定站位後才有意義，呼叫 pda5284 `StopLocationDyna` 拿即時 ETA）

單一 agent 失敗不會讓整個請求失敗（`Contribution.Degraded`），例如 Transit 查詢逾時時，Profile/Geo 的結果仍會用上，回應會降級而不是整個報錯。逾時上限統一 8 秒（`agentTimeout`，足夠涵蓋 Vision 的 Gemini 呼叫）。

`/api/navigate`、`/api/voice_route` 不經過這個 Blackboard 編排器，是各自獨立的 handler，直接呼叫對應 skill。

### Skill（`internal/skill`）

每個 skill 同時提供型別安全呼叫（後端內部用）與 JSON 呼叫（`Invoke`，給 LLM function-calling 用）：

```go
type Skill interface {
    Name() string
    Description() string
    InputSchema() map[string]any
    Invoke(ctx context.Context, raw json.RawMessage) (any, error)
}
```

`Registry` 是名稱 → skill 的查找表，`GET /api/skills` 把所有 schema 攤平回傳（供 Live Guide 的 Gemini Live session 做 tool-calling 用）。目前註冊的 skill：`nearest_stops`、`vision_read_sign`、`stop_eta`、`route_extract`、`navigate`、`tts`、`stt`、`live_guide`。

### Live Guide（`internal/httpapi/live_guide.go` + `internal/skill/live_guide.go`）

`GET /api/live_guide` 是整個系統裡最不一樣的端點：不是「一次請求一次回應」，而是跟相機同壽命的 WebSocket，背後掛一個 Gemini Live session。

- 純 binary/text WebSocket，沒有 JSON 包裝：`0x00+JPEG`＝影格、`0x01+PCM`＝麥克風音訊、`0x02+JSON`＝GPS 座標；server → client 只有純文字（要唸的句子）
- 影格持續送給模型當背景上下文，**不會自己觸發模型回合**（Live API 的回合機制是為語音對話設計的）；後端另開一個 4 秒週期的計時器主動送出判斷提示，才會真的拿到模型輪替
- 讀取 loop 與判斷 loop 是兩個獨立 goroutine，共用一個「任一方結束就關閉整條連線」的機制
- WebSocket Origin 白名單（`CheckOrigin`）預設只放行正式網域與 `localhost`/`127.0.0.1`，可用 `LIVE_GUIDE_ALLOWED_ORIGINS` 環境變數（逗號分隔）加開發用來源，防止惡意網站跨站盜連消耗 Gemini 額度
- 實測限制：Vertex AI 的 Live API 這個專案只有 `location=global` 可用，其餘 region 一律 404；model 為 `gemini-live-2.5-flash`

完整 wire protocol、其餘端點的請求/回應範例，見 `backend/api.md`（維護 API 契約細節的地方，本文件不重複）。

---

## API 設計

端點列表與精確的請求/回應 JSON schema 以 **`backend/api.md`** 為準（含每個端點的實測備註），此處只列端點總覽：

| 方法 | 路徑 | 說明 |
|------|------|------|
| GET | `/healthz` | 健康檢查 |
| POST | `/api/analyze` | 拍照/GPS → 站牌辨識 + ETA |
| POST | `/api/voice_route` | 語音或文字選路線 |
| POST | `/api/navigate` | 目的地 → 同站直達路線規劃 |
| GET | `/api/live_guide` | WebSocket，Gemini Live 即時視覺協助 |
| GET/POST | `/api/profile` | 使用者無障礙偏好 |
| POST | `/api/notify_driver` | 乘客通知駕駛 |
| GET | `/api/driver_alerts` | 司機查詢近期告警 |
| GET | `/api/skills` | Skill registry 內省（除錯/未來 LLM tool-calling 用） |

---

## 數據流

### 場景：乘客拍照 / 持續分析

```
瀏覽器                          Nginx Gateway              Go API (Gin)
  │  POST /api/analyze              │                          │
  │  (image + lat/lng[+wanted_route])│                          │
  ├─────────────────────────────────▶                          │
  │                                  ├─────────────────────────▶│
  │                                  │                          ├─ Wave 1（平行）：
  │                                  │                          │   GeoAgent      → 候選站位
  │                                  │                          │   VisionAgent   → Gemini 讀站牌（有圖才跑）
  │                                  │                          │   ProfileAgent  → 使用者偏好
  │                                  │                          ├─ pickStation：用 Vision 結果從候選中鎖定一個
  │                                  │                          ├─ Wave 2：TransitAgent → pda5284 即時 ETA
  │                                  │                          ├─ 依 wanted_route 排序、標記、產生 voice_summary
  │                                  │                          ├─ tts skill → Cloud TTS 合成（SHA-256 快取，重複文字不重打）
  │                                  │◀─────────────────────────┤
  │◀─────────────────────────────────┤  JSON（station/buses/voice_summary/voice_audio_url）
  │  播放語音、渲染安全視野窗           │                          │
```

### 場景：Live Guide（相機持續開啟）

```
瀏覽器 (passenger.js)                                    Go API                    Vertex AI Live
  │  WebSocket GET /api/live_guide (Origin 檢查) ─────────────▶│                          │
  │                                                            ├─ 開一個 Gemini Live session ─▶│
  │  每 1-2s：0x00+JPEG frame ────────────────────────────────▶├─ PushFrame（背景上下文，不等回覆）
  │  持續：0x01+PCM audio ─────────────────────────────────────▶├─ PushAudio
  │  GPS 更新：0x02+JSON ──────────────────────────────────────▶├─ SetGPS
  │                                                            │  每 4s：RequestJudgment（主動觸發一輪回合，
  │                                                            │  可帶最近站位 ETA 背景資訊）        ├─▶│
  │                                          （多數 tick 無回覆，只有模型判斷「值得講」才有）◀┤
  │◀──────────────────────── 純文字，要唸的一句話 ──────────────┤                          │
  │  裝置 TTS 唸出                                              │                          │
```

---

## 部署架構

### 本機開發

```bash
docker compose up --build
# gateway  → http://localhost:8080  （nginx，HTTP-only，見 nginx.local.conf）
# api      → 容器內部 8080（不對外開 port，只能經 gateway 存取；除錯需要時：
#             docker compose run --service-ports api）
# frontend → 容器內部 5000（同上，只能經 gateway 存取）
```

`docker-compose.yml` 是三個環境（本機 / 正式站）共用的基礎設定，環境差異一律放進**各自的 `docker-compose.override.yml`**（機器本地檔案，不進 git，Compose 會自動疊加）：

| 差異項目 | 本機 override | 正式站 override |
|---------|---------------|-----------------|
| gateway 掛載的 nginx 設定 | `nginx.local.conf`（純 HTTP，無憑證需求） | 無需 override，`nginx.conf` 本身即含 HTTPS |
| `api` 的 GCP 憑證 | 掛載本機 `gcloud auth application-default login` 產生的 ADC | 掛載專用的憑證檔案（見下） |
| `LIVE_GUIDE_ALLOWED_ORIGINS` | 視需要加開發用來源（如 Tailscale Serve 網址） | 不需要，正式網域已在白名單預設值 |

需要用手機實機測試相機功能時（`getUserMedia` 要求 secure context，區網 IP 走 HTTP 不會跳權限請求），本機用 **Tailscale Serve** 把 `localhost:8080` 以受信任 HTTPS 分享到 tailnet：`tailscale serve --bg --https=443 http://localhost:8080`，不用時 `tailscale serve --https=443 off` 關閉。

### 正式環境

單一 GCE VM（`asia-east1-b`），沒有 Cloud Run / Load Balancer / 容器編排系統：

```
GCE VM（Ubuntu，開機碟 10G + 另掛一顆較大的資料碟給 Docker 用）
├─ docker compose（3 容器）
│   ├─ gateway (nginx:1.27-alpine)
│   │   ├─ 監聽 80/443
│   │   ├─ 80 → 重導 443（/.well-known/acme-challenge 例外，讓續證走得通）
│   │   └─ 443 → TLS 終止，反向代理到 api / frontend
│   ├─ api (Go, 由 backend/Dockerfile 建置)
│   └─ frontend (Flask, 由 frontend.Dockerfile 建置)
├─ certbot（直接裝在主機上，不在容器內）
│   ├─ 憑證：/etc/letsencrypt/live/devjam.justin0711.com/
│   │        （standalone 模式取得，掛進 gateway 容器 :ro）
│   └─ systemd timer 自動續期
└─ GCP 防火牆規則 allow-http-https：允許 0.0.0.0/0 進 80/443
    （預設只開 22/3389，80/443 需另外建立，否則 certbot 的 HTTP-01
      驗證會逾時失敗）
```

**GCP 憑證來源**：這個專案掛在共用的教學/實驗室 GCP 專案（`gcplab.me` 網域帳號），帳號雖有 `roles/owner` 但專案層級 IAM 政策被鎖死（無法 `setIamPolicy`），因此無法建立、授權新的 service account。實務作法是把已登入帳號的個人 ADC 憑證複製到主機固定路徑，由 `api` 服務的 override 掛載使用——不是最小權限的做法，但在這個帳號的限制下是唯一可行的路徑。

**部署更新流程**：主機上 `cd ~/devjam && git pull && docker compose up --build -d`（`gateway` 若只是設定變更可以只重建它：`docker compose up -d gateway`）。

---

## 核心組件詳解

### stopindex：站位空間索引

pda5284 從不公開站牌座標，`internal/stopindex` 服務的資料完全來自 `cmd/indexer` 離線批次跑出來的 `data/stops.json`：對每條設定的路線爬 `route.jsp` 拿站牌中繼資料，用 `stop.jsp` 的重導向把單一站牌（sid）對應到聚合站位（slid），再持續輪詢 `RouteDyna` 抓靜止公車的 GPS 當作該站的座標樣本。這是一次性批次工作（每次執行從頭收樣本、覆蓋輸出檔），不是常駐排程；`cmd/server` 開機時把產出的 JSON 整個讀進記憶體，之後的站位查詢（`Nearest`）是純記憶體線性掃描，城市規模的站點數量下仍是次毫秒級，刻意不放進請求的網路路徑。

### profile.Store：使用者偏好

目前是行程記憶體內的 map，容器重啟即清空（程式碼註解明講：這是 Firestore-backed store 的替身，等真的有 GCP 憑證可測時再換）。`api.md` 也明確記載告警（driver alerts）走同樣的記憶體儲存模式。

### pda.Client：外部資料源

`https://pda5284.gov.taipei/MQS/` 是台北市公開、無需金鑰的公車動態 API，`pda.Client` 自我限速在 ~2 req/s（500ms ticker），加上識別用的 User-Agent，作為一個「禮貌」的爬蟲對待，不因為沒有官方 rate limit 公告就無限制打。

---

## 已知的技術債 / 尚未實作

以下項目出現在早期規劃文件（`plan.md`、本文件的 1.0 版）中，但目前**沒有**被建置，記錄下來避免未來又被舊文件誤導：

- **Firestore / Redis**：規劃階段設計的持久化層，實際上 profile 與司機告警都是記憶體內狀態，沒有資料庫。曾經在正式主機上手動裝過一份原生 Redis（非 docker-compose 管理），2026-08-18 確認完全沒有程式碼在用之後已停用並清空資料。
- **Cloud Run / Cloud Load Balancer / Cloud Tasks / Cloud Scheduler**：規劃階段設想的雲端原生部署，實際部署是單一 GCE VM + Docker Compose + 主機上的 certbot，見〈部署架構〉。
- **Google Routes/Places API 行程規劃**：`/api/navigate` 明確只處理「同一條路線直達」，用專案自己的 stopindex + pda5284 資料做，沒有申請 Routes API 金鑰；需要轉乘時會明確告知使用者，不會生成無法驗證的假路線。
- **`server.py` / `firestore_client.py` / `pipeline.py`（repo 根目錄）+ 根目錄 `Dockerfile`**：這是專案更早期的原型（單一 Flask 應用 + Firestore），已被 `backend/`（Go）+ `frontend_server.py`（Flask，僅模板/靜態檔）取代。`docker-compose.yml` 完全沒有引用根目錄 `Dockerfile`，這組檔案目前不在任何部署路徑上，僅供參考或待清理。
