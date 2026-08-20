# 城市之眼（ReVision City）

智慧城市無障礙公車助手。為視覺障礙使用者（隧道 視野、中心視野缺損）設計：
辨識公車站牌、查詢即時到站時間、以語音與「安全視野窗」呈現資訊，並在候車
與上車過程中持續提供即時視覺協助。

## 功能

- **拍照 / GPS 站牌辨識 + 到站時間**：拍照或持續 GPS 定位，辨識站牌並回傳
  當前站位所有路線的到站時間（`POST /api/analyze`）。
- **語音選路線**：說出或輸入想搭的路線號，後續分析會優先標記該路線
  （`POST /api/voice_route`）。
- **導航規劃**：輸入目的地，找出同站直達路線與逐步引導文字
  （`POST /api/navigate`）。
- **Live Guide**：相機持續開啟時，Gemini Live 主動觀看畫面，在判斷有必要
  時開口提示（公車進站、車門位置、危險等）（`GET /api/live_guide`，WebSocket）。
- **個人偏好校準**：校準安全視野窗位置、字型縮放、語音開關，之後自動套用
  （`GET/POST /api/profile`）。
- **司機告警**：乘客通知駕駛需要協助，司機端輪詢查看
  （`POST /api/notify_driver` / `GET /api/driver_alerts`）。

## 技術棧

- **前端**：Flask 伺服 Jinja2 模板 + Vanilla JavaScript + CSS3
  （`frontend_server.py`、`templates/`、`static/`）。
- **網關**：Nginx，TLS 終止、反向代理、WebSocket Upgrade 轉發（`nginx.conf`）。
- **後端**：Go（Gin），Agent/Skill 架構（`backend/`）。
- **AI 服務**：Google Vertex AI —— Gemini（視覺辨識與推理）、Gemini Live
  （即時視覺協助）、Cloud Text-to-Speech、Cloud Speech-to-Text v2。
- **數據源**：台北市公車動態系統 pda5284（公開、無金鑰，自我限速約 2 req/s）。
- **狀態**：行程記憶體內狀態（使用者偏好與司機告警），無資料庫。

## 快速開始

### 完整服務（Docker Compose）

```bash
docker compose up --build
```

- 乘客端：<http://localhost:8080>
- 司機端：<http://localhost:8080/driver>

服務分工：

- `gateway`：Nginx，提供同源入口並把 `/api/*` 代理到 Go API。
- `frontend`：Flask，只負責模板與靜態檔案。
- `api`：Go 後端，提供分析、個人偏好、通知與警示 API。

未設定 Google Cloud 憑證時 API 仍可啟動：Gemini 視覺辨識、TTS、STT 會自動
降級，但 GPS/站牌查詢與前端頁面仍可使用。

### 後端（本地）

```bash
cd backend
go run ./cmd/server
```

預設監聽 `:8080`，並載入站位索引（`STOPS_INDEX_PATH`，預設 `data/stops.json`）。
若索引不存在，先執行離線批次工具建立：

```bash
go run ./cmd/indexer
```

### 前端（本地）

```bash
python frontend_server.py
```

預設監聽 `:5000`，伺服 `templates/` 與 `static/`。

## 環境變數

後端讀取的環境變數（含預設值）：

| 變數 | 預設 | 用途 |
|------|------|------|
| `PORT` | `8080` | API 監聽埠 |
| `STOPS_INDEX_PATH` | `data/stops.json` | 站位索引路徑 |
| `GCP_PROJECT` | `devjam26aug17tpe-1280` | Vertex AI 專案 |
| `GCP_LOCATION` | `us-central1` | Vertex AI 區域 |
| `TTS_BUCKET` | `devjam26aug17tpe-1280-tts-audio` | 語音 mp3 快取 bucket |
| `STT_LOCATION` | `global` | Speech-to-Text 區域 |
| `STT_MODEL` | `latest_long` | Speech-to-Text 模型 |
| `LIVE_GUIDE_ALLOWED_ORIGINS` | （正式網域 + localhost） | Live Guide WebSocket Origin 白名單（逗號分隔） |

GCP 憑證透過 Application Default Credentials 掛載；`docker-compose.yml` 是
本機/正式站共用的基礎設定，環境差異（憑證路徑、nginx 設定檔、Origin 白名單）
由各環境的 `docker-compose.override.yml` 處理。

本機要用手機測試相機（`getUserMedia` 需 secure context）時，可用
`tailscale serve --bg --https=443 http://localhost:8080` 提供受信任 HTTPS。

## API 端點

| 方法 | 路徑 | 說明 |
|------|------|------|
| GET | `/healthz` | 健康檢查 |
| POST | `/api/analyze` | 拍照 / GPS 站牌辨識 + 到站時間 |
| POST | `/api/voice_route` | 語音或文字選路線 |
| POST | `/api/navigate` | 目的地同站直達路線規劃 |
| GET | `/api/live_guide` | WebSocket，Gemini Live 即時視覺協助 |
| GET/POST | `/api/profile` | 讀寫使用者無障礙偏好 |
| POST | `/api/notify_driver` | 乘客通知駕駛 |
| GET | `/api/driver_alerts` | 司機查詢近期告警 |
| GET | `/api/skills` | Skill registry 內省（LLM tool-calling / 除錯） |

完整的請求/回應 JSON schema 與實測備註見 [`backend/api.md`](backend/api.md)。

### Live Guide 通訊協定

`GET /api/live_guide` 是長生命週期的 WebSocket（與相機同壽命），純 binary/text
封包，無 JSON 包裝：

- `0x00 + JPEG`：相機影格
- `0x01 + PCM`：麥克風音訊
- `0x02 + JSON`：GPS 座標（`{"lat":…, "lng":…}`）
- server 端回傳純文字：要唸出的一句提示

## 專案結構

```
backend/
  cmd/server        API 進入點：組出 skill/agent/handler 並啟動 Gin
  cmd/indexer       離線批次工具：爬 pda5284 產生 data/stops.json
  internal/httpapi  Gin handler 層（一個檔案對一個端點）
  internal/agent    Blackboard + Wave 同步編排器
  internal/skill    可被 agent 與 LLM function-calling 呼叫的最小業務單元
  internal/pda      pda5284.gov.taipei HTTP client（自我限速）
  internal/stopindex 站位空間索引（啟動時載入記憶體）
  internal/profile  使用者無障礙偏好（記憶體 store）
  api.md            API 契約細節
  test_all.sh       全功能 API 測試（bash + curl）
  test_all.py       API 測試（Python）
frontend_server.py  Flask，伺服模板與靜態檔案
nginx.conf          正式環境 Nginx（HTTPS + 反向代理）
nginx.local.conf    本機 Nginx（HTTP-only）
docker-compose.yml  三容器編排（gateway / frontend / api）
templates/          Jinja2 模板（乘客端、司機端、共用版型）
static/             CSS 與 Vanilla JavaScript
.qa/                Playwright E2E 測試
```

### 分析流程（`POST /api/analyze`）

單一請求內同步完成的 Blackboard 編排：

- **Wave 1（平行）**：GeoAgent（GPS 半徑內候選站位）／VisionAgent（有照片才跑，
  Gemini 讀站牌）／ProfileAgent（讀使用者偏好）。
- 依 Vision 結果從候選中鎖定位（`pickStation`）。
- **Wave 2**：TransitAgent 呼叫 pda5284 拿即時 ETA。
- 依 `wanted_route` 排序、標記，產生 `voice_summary`，並合成語音
  （`voice_audio_url`）。

單一 agent 失敗會讓回應降級而非整個報錯。

## 測試

啟動服務後：

```bash
# API 全功能測試（bash + curl）
bash backend/test_all.sh

# 或 Python 版
python backend/test_all.py

# Playwright E2E（需 npm install 並執行 npx playwright test）
npx playwright test
```

`backend/test_all.sh` / `test_all.py` 對 `http://localhost:8080` 逐端點驗證；
`.qa/` 下的 Playwright spec 覆蓋 Live Guide、手動上傳、語音選線、站牌校準等前端流程。

## 文件

- [`ARCHITECTURE.md`](ARCHITECTURE.md)：完整技術架構、數據流與部署說明。
- [`backend/api.md`](backend/api.md)：API 契約細節。
- [`DESIGN.md`](DESIGN.md)：前端設計系統（配色、字型、間距、元件）。

> 根目錄的 `server.py` / `pipeline.py` / `firestore_client.py` 與 `Dockerfile`
> 是較早期的單一 Flask + Firestore 原型，已被 `backend/`（Go）與
> `frontend_server.py`（Flask，僅模板/靜態檔）取代；`docker-compose.yml`
> 未引用根目錄 `Dockerfile`，這組檔案僅供參考。
