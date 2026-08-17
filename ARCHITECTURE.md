# 城市之眼 — 技術架構文檔

> 無障礙公車助手系統的完整技術設計

**版本**：1.0  
**最後更新**：2026-08-17

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

---

## 系統概覽

### 產品定位

城市之眼是一個針對身心障礙人士（特別是隧道視野患者）設計的無障礙公車助手，結合視覺辨識、GPS 定位、AI 語音合成，幫助使用者快速識別當前站位並獲得即時到站資訊。

### 核心功能

| 功能 | 說明 | 角色 |
|-----|------|------|
| **站牌視覺辨識** | 使用 Gemini Vision 讀取手機相機中的站牌名稱與路線 | 乘客 |
| **GPS 定位** | 透過手機 GPS 確定最近的站位 | 乘客 |
| **即時 ETA** | 查詢當前站位所有路線的到站時間 | 乘客 |
| **個人偏好** | 校準視障人士的「安全視野窗」位置、字型大小、語音啟用 | 乘客 |
| **司機告警** | 將乘客需求通知駕駛，協助無障礙上車 | 司機 |
| **多路線聚合** | 一個站位通常有 10+ 條路線，系統智能排序推薦 | 乘客 |

### 技術棧概要

- **前端**：HTML5 + Vanilla JavaScript + CSS3（响應式設計）
- **前端框架**：Flask（模板 + 靜態檔案伺服）
- **網關層**：Nginx（反向代理、跨域整合）
- **後端**：Go 1.20+（Gin 框架）
- **AI 服務**：Google Vertex AI Gemini（視覺、推理、語音）
- **數據源**：台北市公車動態系統 pda5284
- **基礎設施**：Docker Compose 本地開發 / Google Cloud Run 生產
- **數據存儲**：Firestore（個人資料）、Redis（即時 ETA 快取）

---

## 整體架構

```
┌─────────────────────────────────────────────────────────────────┐
│                        用戶界面層                                 │
│  ┌──────────────────┐  ┌─────────────────────────────────────┐  │
│  │  乘客端(Passenger)│  │      司機端(Driver)                  │  │
│  │  - 相機/GPS      │  │  - 路線選擇                         │  │
│  │  - 實時通知      │  │  - 告警列表                         │  │
│  └──────────────────┘  └─────────────────────────────────────┘  │
└─────────────────────┬──────────────────────────────────────────┘
                      │ HTTP(S)
┌─────────────────────▼──────────────────────────────────────────┐
│                    Nginx 網關層                                  │
│  - 同源 HTTP 入口 (localhost:8080)                              │
│  - /api/* → Go API (背後)                                       │
│  - CORS 中介                                                    │
└─────────────────────┬──────────────────────────────────────────┘
                      │
        ┌─────────────┴────────────────┬──────────────────┐
        ▼                              ▼                  ▼
┌──────────────────┐  ┌──────────────────────┐  ┌──────────────┐
│  Flask 容器      │  │   Go API 容器        │  │  pda5284 爬蟲│
│  - 前端模板      │  │   (Gin 框架)         │  │  (定時 Job)  │
│  - 靜態資源      │  │   - 核心業務邏輯     │  │              │
│  - 簡單路由      │  │   - Agent 編排       │  │              │
└──────────────────┘  │   - Skill Registry   │  └──────────────┘
                      │   - 快速路徑(Fast)   │
                      │   - 非同步路徑       │
                      └──────────────────────┘
                              │
        ┌──────────────────────┼──────────────────────┐
        ▼                      ▼                      ▼
┌──────────────────┐  ┌──────────────────────┐  ┌──────────────┐
│   Firestore      │  │   Cloud Storage      │  │   Redis      │
│  - 用戶 Profile  │  │  - 站位索引 JSON     │  │  - ETA 快取  │
│  - Session State │  │  - 語音音檔          │  │  - TTL: 15s  │
└──────────────────┘  └──────────────────────┘  └──────────────┘
                              ▲
        ┌──────────────────────┼──────────────────────┐
        ▼                      ▼                      ▼
┌──────────────────┐  ┌──────────────────────┐  ┌──────────────┐
│  Gemini Vision   │  │ Cloud Text-to-Speech │  │  Routes API  │
│ (站牌文字辨識)    │  │  (語音合成)           │  │ (行程規劃)    │
└──────────────────┘  └──────────────────────┘  └──────────────┘
```

---

## 前端架構

### 頁面結構

#### 乘客端 (`/` 或 `/passenger`)

```html
<div class="page passenger">
  <!-- 1. 相機取景區 -->
  <section class="camera-wrap">
    <video id="camera-preview"></video>
    <div class="camera-hint">請對準站牌</div>
    <button class="shutter-btn">拍照</button>
  </section>

  <!-- 2. 手動上傳區（備用） -->
  <section class="manual-upload">
    <input type="file" accept="image/*">
    <label>
      <input type="checkbox" class="test-gps-toggle"> 
      使用測試位置（新益里）
    </label>
    <div class="file-meta">已選擇：...</div>
    <div aria-live="polite" class="upload-status"></div>
  </section>

  <!-- 3. 結果展示區 -->
  <section class="safe-zone-window">
    <!-- 站牌 + 路線 -->
    <h2>{{station_name}}</h2>
    
    <!-- 最近班次（高亮） -->
    <div class="safe-zone-highlight">
      路線 {{route}} | {{eta_minutes}} 分鐘 | {{direction}}
    </div>

    <!-- 可展開的完整清單 -->
    <details class="bus-list">
      <summary>更多路線 ({{count}})</summary>
      <ul>
        <li v-for="bus in buses">
          {{bus.route}} | {{bus.eta_minutes}}分 | {{bus.direction}}
        </li>
      </ul>
    </details>

    <!-- 操作按鈕 -->
    <div class="action-cluster">
      <button class="btn-primary">通知駕駛</button>
      <button class="btn-secondary">設定</button>
    </div>
  </section>

  <!-- 4. 設定對話框（模態） -->
  <dialog class="calibration-dialog">
    <!-- 校準向導 -->
  </dialog>
</div>
```

#### 司機端 (`/driver`)

```html
<div class="page driver">
  <!-- 1. 路線選擇 -->
  <section class="route-selector">
    <input type="text" placeholder="輸入路線號（如 307）">
    <button>查詢</button>
  </section>

  <!-- 2. 告警列表 -->
  <section class="alerts-panel">
    <div class="alert-item" v-for="alert in alerts">
      <div class="alert-badge">{{alert.impairment_type}}</div>
      <div class="alert-content">
        <p>{{alert.station_name}}</p>
        <small>{{alert.timestamp}}</small>
      </div>
      <button class="btn-acknowledge">已確認</button>
    </div>
  </section>
</div>
```

### 設計系統

#### 色彩方案

| 用途 | Token | 值 | 應用 |
|-----|-------|-----|------|
| 背景 | `--color-bg` | `#F7F5F0` | 頁面背景（米白） |
| 表面 | `--color-surface` | `#FFFFFF` | 卡片、控制項 |
| 正文 | `--color-text` | `#14181F` | 標題、本文 |
| 主要操作 | `--color-primary` | `#C8850C` | 按鈕、安全視野高亮（琥珀色） |
| 次要操作 | `--color-secondary` | `#1D5FB8` | 焦點圈、輔助按鈕 |
| 錯誤 | `--color-danger` | `#C62828` | 錯誤訊息 |
| 成功 | `--color-success` | `#1E8E3E` | 完成確認 |

#### 排版與間距

- **字體棧**：`-apple-system`, `BlinkMacSystemFont`, `Noto Sans TC`, `PingFang TC`, sans-serif
- **可縮放**：根據全局 `--font-scale` 調整（使用者可在設定中調大）
- **間距節奏**：8px 基準（`--space-1` = 8px, `--space-2` = 16px, ...）
- **觸控目標**：最小 48px（WCAG 2.2 AA）
- **單欄寬度上限**：乘客端 480px，司機端 560px
- **響應式下限**：375px 無需橫向捲動

#### 動畫與無障礙

- **核心動畫**：150–200ms 色彩/不透明度反饋（按鈕、狀態）
- **掃描脈衝**：1500ms 迴圈（相機掃描指示）
- **敬重動畫偏好**：`prefers-reduced-motion: reduce` 移除非必要動畫

---

## 後端架構

### 運行時分層

```go
// 核心四層堆疊

Layer 1: HTTP API (Gin 路由)
    ├─ /api/analyze      [POST]  快速路徑 + 非同步觸發
    ├─ /api/profile      [GET/POST]  個人偏好
    └─ /api/driver_alerts [GET]  司機告警列表

    ↓ 控制流

Layer 2: Session 狀態機
    ├─ 讀 Firestore / Redis 查目前的 locked_slid、watch_list
    ├─ 決策：執行「快速路徑」或「完整 DAG」
    └─ 寫回 Firestore / Redis（狀態更新）

    ↓ 業務邏輯

Layer 3: Agent 編排 + Skill 註冊表
    ├─ 多 Agent 平行執行 (errgroup)
    ├─ Wave 1: Vision ∥ Geo ∥ Journey
    ├─ Wave 2: Transit
    └─ Wave 3: Narrator

    ↓ 數據層

Layer 4: 外部系統集成
    ├─ pda5284  (公車 ETA 實時數據)
    ├─ Gemini   (Vision / LLM)
    ├─ Routes API (行程規劃)
    ├─ Cloud TTS (語音合成)
    └─ Firestore / Redis (狀態存儲)
```

### 雙路徑設計

#### 快速路徑 (Fast Path)

**觸發條件**：
- 已鎖定站位 (`locked_slid` 存在)
- GPS 位移 ≤ 30m
- 距上次完整分析 ≤ 60s

**執行內容**（無 LLM）：
```go
1. 讀 session.locked_slid
2. 調用 Transit Agent → StopLocationDyna(slid)
3. 排序路線、應用 watch_list 過濾
4. 計算 urgency
5. 回傳 {status: "success", buses[], ...}
```

**性能目標**：p95 < 800ms

#### 代理路徑 (Agent Path - 非同步)

**觸發條件**：
- 新使用者、未鎖定站位
- GPS 位移 > 30m（可能換站）
- 手動上傳影像

**執行內容**（完整 DAG）：
```
入隊 Cloud Tasks 任務 → 後台執行：
  Wave 1:
    Vision Agent (Gemini Vision) ∥ 
    Geo Agent (空間索引) ∥ 
    Journey Agent (Gemini + Routes API)

  Orchestrator 決定 locked_slid

  Wave 2:
    Transit Agent (StopLocationDyna)

  Wave 3:
    Narrator Agent (Gemini + TTS)

  寫回 Firestore session state
```

### Agent 系統設計

#### 核心 Agent 列表

| Agent | 需求 | 輸入 | 輸出 | 耗時 |
|-------|------|------|------|------|
| **Vision** | Gemini Vision API | 照片幀 | 站名、路線、方向 | 1–2s |
| **Geo** | 本地索引計算 | GPS (lat/lng) | 距離內候選站位 | <100ms |
| **Transit** | pda5284 API | `slid` | 各路線 ETA、公車位置 | 300–500ms |
| **Journey** | Gemini + Routes API | 目的地、Profile | `watch_list`（推薦路線） | 2–3s |
| **Narrator** | Gemini + Cloud TTS | 上游全部結果 | 語音文字、語音 URL、urgency | 1–2s |

### Skill 設計

每個 Skill 同時支持**類型安全調用**（後端內部）與 **JSON 調用**（LLM function calling）：

```go
type Skill interface {
    Name() string
    Description() string
    InputSchema() map[string]any              // JSON Schema 餵 Gemini
    Invoke(ctx context.Context, raw json.RawMessage) (any, error)
}

// 範例：StopETA Skill
type StopETA struct { pda *pda.Client }

// 類型安全調用（後端 agent）
func (s *StopETA) Do(ctx context.Context, in StopETAInput) (StopETAOutput, error) {
    return s.pda.StopLocationDyna(ctx, in.SlID)
}

// JSON 調用（Gemini function calling）
func (s *StopETA) Invoke(ctx context.Context, raw json.RawMessage) (any, error) {
    var in StopETAInput
    json.Unmarshal(raw, &in)
    return s.Do(ctx, in)
}
```

---

## API 設計

### 1. POST /api/analyze

**乘客端分析端點**

#### 請求

```json
{
  "session_id": "user_123_session_456",
  "location": {
    "lat": 25.0141,
    "lng": 121.5341,
    "accuracy_m": 8
  },
  "image": "data:image/jpeg;base64,/9j/4AAQSkZJ...",
  "test_gps": false
}
```

#### 回應 (成功)

```json
{
  "status": "success",
  "station_name": "新益里",
  "station_id": 19111,
  "distance_m": 35,
  "buses": [
    {
      "route": "307",
      "eta_minutes": 3,
      "direction": "往台北車站",
      "urgency": "high",
      "in_watch_list": true,
      "vehicle_id": "222240644"
    },
    {
      "route": "202",
      "eta_minutes": 8,
      "direction": "往公館",
      "urgency": "medium",
      "in_watch_list": false
    }
  ],
  "display": {
    "safe_zone_position": "top",
    "font_scale": 1.5
  },
  "voice_summary": {
    "text": "307 路還有 3 分鐘進站，往台北車站",
    "audio_url": "https://storage.googleapis.com/..."
  }
}
```

#### 回應 (未找到)

```json
{
  "status": "not_found",
  "message": "尚未偵測到站牌",
  "distance_m": 245,
  "suggestion": "請向前走至公車站牌附近"
}
```

### 2. GET /api/profile

**查詢使用者個人偏好**

#### 回應

```json
{
  "exists": true,
  "user_id": "user_123",
  "impairment_type": "tunnel_vision",
  "safe_zone": {
    "x": 50,
    "y": 20,
    "radius": 30
  },
  "font_scale": 1.5,
  "voice_enabled": true,
  "watch_routes": ["307", "202", "630"],
  "created_at": "2026-08-01T10:30:00Z",
  "last_calibrated": "2026-08-15T14:22:00Z"
}
```

### 3. POST /api/profile

**建立或更新使用者個人偏好**

#### 請求

```json
{
  "impairment_type": "tunnel_vision",
  "safe_zone": { "x": 50, "y": 20, "radius": 30 },
  "font_scale": 1.5,
  "voice_enabled": true,
  "watch_routes": ["307", "202"]
}
```

### 4. POST /api/notify_driver

**乘客通知駕駛**

#### 請求

```json
{
  "route": "307",
  "station_name": "新益里",
  "impairment_type": "tunnel_vision"
}
```

#### 回應

```json
{
  "status": "success",
  "alert_id": "alert_1692259411_307"
}
```

### 5. GET /api/driver_alerts?route=307

**司機查詢最近告警**

#### 回應

```json
{
  "route": "307",
  "alerts": [
    {
      "alert_id": "alert_1692259411_307",
      "station_name": "新益里",
      "impairment_type": "tunnel_vision",
      "timestamp": "2026-08-17T08:43:31Z",
      "acknowledged": false
    }
  ]
}
```

---

## 數據流

### 場景 1：乘客拍照識別

```
時間軸：

T+0ms   ├─ 使用者點擊 [拍照]
        │
T+50ms  ├─ 前端獲得相機幀 + 讀取設備 GPS
        │
T+60ms  ├─ 前端組包：base64(image) + GPS → POST /api/analyze
        │
T+70ms  ├─ Go 後端收到請求
        │  ├─ 讀 session state（有無 locked_slid）
        │  ├─ 決策：快速路徑 OR 完整 DAG
        │  └─ (若需完整) 入隊 Cloud Tasks
        │
        ├─ 快速路徑執行（已鎖定站位）
        │  ├─ Transit Agent: slid → StopLocationDyna API
        │  ├─ 排序 + urgency 計算
        │  └─ 800ms 內回傳
        │
T+870ms ├─ 或
        │
        ├─ 完整 DAG（新站位或超時）
        │  ├─ 立即回傳 HTTP 202 (Accepted)
        │  └─ 後台並行：Vision ∥ Geo ∥ Journey
        │
T+100ms ├─ 前端收到回應
        │  ├─ status='success' → 渲染站牌 + 路線
        │  ├─ 播放語音（若啟用）
        │  └─ 設定 5 秒後自動刷新
        │
T+5100ms├─ 自動刷新 (下一幀)
```

---

## 部署架構

### 本地開發環境

```bash
# 啟動完整棧
docker compose up --build

# 自動暴露的端口：
# - localhost:8080   (Nginx 網關，乘客 + 司機端)
# - localhost:8000   (Go API，直接用於開發測試)
# - localhost:6379   (Redis，可選連接)
```

**docker-compose.yml 服務清單**：

| 服務 | 映像 | 用途 |
|------|------|------|
| `gateway` | nginx:latest | HTTP 反向代理、跨域、靜態檔案 |
| `frontend` | Python:3.9 + Flask | 模板引擎、靜態檔案伺服 |
| `api` | golang:1.20 → 自訂 | Gin 後端、業務邏輯 |
| `redis` | redis:7-alpine | ETA 快取、session 存儲 |

### 生產環境（Google Cloud）

```
┌────────────────────────────────────────────┐
│          Google Cloud Platform             │
├────────────────────────────────────────────┤
│                                            │
│  ┌──────────────────────────────────────┐ │
│  │   Cloud Run Service (API)            │ │
│  │   - Go 容器                          │ │
│  │   - min_instances=1 (冷啟動優化)     │ │
│  │   - 內存: 512MB, CPU: 1              │ │
│  └─────────┬──────────────────────────┘ │
│            │                            │
│  ┌─────────▼──────────────────────────┐ │
│  │   Cloud Load Balancer + CDN        │ │
│  │   - 全球邊界快取靜態資源            │ │
│  │   - HTTPS / 自簽憑證                │ │
│  └──────────────────────────────────┘ │
│                                        │
│  ┌──────────────────────────────────┐ │
│  │   Firestore                      │ │
│  │   - 使用者 Profile               │ │
│  │   - Session State (可選)         │ │
│  └──────────────────────────────────┘ │
│                                        │
│  ┌──────────────────────────────────┐ │
│  │   Memorystore Redis              │ │
│  │   - ETA 快取 (TTL 15s)          │ │
│  │   - 限流計數器                    │ │
│  └──────────────────────────────────┘ │
│                                        │
│  ┌──────────────────────────────────┐ │
│  │   Cloud Storage                  │ │
│  │   - stops.json 站位索引          │ │
│  │   - 語音音檔 (.mp3)              │ │
│  └──────────────────────────────────┘ │
│                                        │
│  ┌──────────────────────────────────┐ │
│  │   Cloud Tasks + Cloud Scheduler  │ │
│  │   - 非同步 Agent 任務入隊         │ │
│  │   - 每日 pda5284 爬蟲 (建表)      │ │
│  └──────────────────────────────────┘ │
│                                        │
│  ┌──────────────────────────────────┐ │
│  │   Vertex AI Gemini API           │ │
│  │   - Vision (站牌辨識)            │ │
│  │   - LLM (推理 + 語音文案)        │ │
│  └──────────────────────────────────┘ │
│                                        │
│  ┌──────────────────────────────────┐ │
│  │   Cloud Text-to-Speech           │ │
│  │   - Chirp3-HD 中文合成            │ │
│  └──────────────────────────────────┘ │
│                                        │
│  ┌──────────────────────────────────┐ │
│  │   Cloud Trace + Cloud Logging    │ │
│  │   - 即時延遲監控 (p95 < 800ms)   │ │
│  │   - 日誌聚合                      │ │
│  └──────────────────────────────────┘ │
└────────────────────────────────────────────┘
         ↑
         │ pda5284 API (台北市公開數據)
         │ https://pda5284.gov.taipei/MQS/
```

---

## 核心組件詳解

### 1. pda5284 集成

#### 數據源概述

| 端點 | 返回 | 用途 |
|-----|------|------|
| `routelist.jsp` | HTML | 全路線清單 |
| `route.jsp?rid=<n>` | HTML | 站序、站名、方向 |
| `stop.jsp?sid=<n>` | 302 重定向 | 站位(slid)查詢 |
| `RouteDyna?routeid=<n>` | JSON | 單路線 ETA + 公車位置 |
| `StopLocationDyna?stoplocationid=<slid>` | JSON | **★ 核心：單站位全路線 ETA** |

#### N1 欄位編碼（ETA 提取）

```
N1, 19111, 15111, 222240644, 19105, ..., 716, 5, 0, ...
    └─── slid  └─ rid    └─ vehicleID    └─ ETA秒
                                          (索引7)
```

#### 解析注意事項

- 頁面編碼為 **Big5**，但站名以 HTML numeric entity 形式（`&#x8706;` = 蘆）
- **容錯設計**：欄位數不足、型別異常時降級回傳部分結果，不 panic
- **禮貌爬取**：限流 2 req/s，設 User-Agent，失敗指數退避
- **快取策略**：Redis TTL 15s，可支撐實時查詢同時保護數據源

### 2. 站位索引構建

#### 離線建表流程

```
1. Cloud Scheduler (每日執行)
   └─ 觸發 Cloud Run Job: cmd/indexer/main.go

2. indexer 執行：
   ├─ 爬 routelist.jsp → 全部 rid
   ├─ 對每個 route：
   │  └─ route.jsp?rid=<n> → (sid, 站名, 方向, 站序)
   ├─ 對每個 stop：
   │  └─ stop.jsp?sid=<n> (302) → slid
   ├─ 座標填充（來源見下表）
   └─ 輸出：stops.json
      {
        "version": "20260817",
        "stops": [
          {
            "slid": 19111,
            "name": "新益里",
            "lat": 25.0141,
            "lng": 121.5341,
            "routes": ["307", "202", "630"]
          }
        ]
      }

3. 上傳 Cloud Storage
   └─ gs://PROJECT_ID/stops.json

4. 服務啟動時：
   ├─ 載入 stops.json 至記憶體
   ├─ 建空間索引（Geohash / S2 Cell）
   └─ 查詢時間複雜度：O(1) ≈ 1ms
```

---

## 目錄結構總覽

```
.
├── backend/                     # Go API 後端
│   ├── cmd/
│   │   ├── server/main.go       # HTTP 服務入口
│   │   └── indexer/main.go      # 離線建表工作
│   ├── internal/
│   │   ├── agent/               # 多 Agent 編排
│   │   │   ├── orchestrator.go  # DAG 執行引擎
│   │   │   ├── vision.go
│   │   │   ├── geo.go
│   │   │   ├── transit.go
│   │   │   ├── journey.go
│   │   │   └── narrator.go
│   │   ├── skill/               # Skill 註冊與實作
│   │   │   ├── registry.go
│   │   │   ├── nearest_stops.go
│   │   │   └── stop_eta.go
│   │   ├── pda/                 # pda5284 客戶端
│   │   │   ├── client.go
│   │   │   ├── dyna.go
│   │   │   └── scrape.go
│   │   ├── stopindex/           # 空間索引 (Geohash/S2)
│   │   ├── session/             # 狀態機 + Firestore
│   │   ├── httpapi/             # Gin 路由與 DTO
│   │   │   ├── router.go
│   │   │   ├── analyze.go
│   │   │   └── profile.go
│   │   └── config/              # 配置管理
│   ├── go.mod
│   ├── go.sum
│   ├── Dockerfile
│   └── test_all.sh
│
├── static/                      # 前端靜態資源（JS/CSS/HTML）
│   ├── css/main.css
│   ├── js/
│   │   ├── common.js
│   │   ├── driver.js
│   │   ├── passenger.js
│   │   └── manual-upload.js
│   └── html/
│
├── docker-compose.yml
├── Dockerfile
├── nginx.conf
├── DESIGN.md                    # UI 設計系統
├── ARCHITECTURE.md              # ★ 本文檔
└── README.md                    # 快速開始
```

---

**文檔維護**：justin  
**最後編輯**：2026-08-17 16:30 UTC+8
