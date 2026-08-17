# 城市無障礙公車 Agent — 實作計畫

> 基於 Google Cloud 的多 agent 城市系統。以**即時性**為目標，協助身心障礙人士順利搭乘公車。
> API 契約見 [api.md](./api.md)。

---

## 0. 需求定義

1. 前端傳來**手機 GPS 定位 + 當前影格圖片**
2. 後端據此找出使用者**最近的公車站位**，再查該站位的路線與到站資訊
3. **離站位太遠 → 回 `status: "not_found"`**
4. 由行程規劃推導出需要搭乘的路線
5. **多個 agent 平行執行不同工作**，由 orchestrator 匯總
6. 每個可呼叫的 API 包裝成 **skill** 供 agent 調用

**本階段不做**：`notify_driver`（保留 endpoint 回 501）、裝置端 OCR。

---

## 1. 資料源：pda5284（已逆向驗證）

改用**台北市公車動態資訊系統** `https://pda5284.gov.taipei/MQS/`，不使用 TDX。
無需 API key、無 OAuth、純 GET。

### 1.1 端點對照

| 端點 | 型態 | 用途 |
|---|---|---|
| `routelist.jsp` | HTML | 全路線清單 → `route.jsp?rid=` |
| `route.jsp?rid=<n>` | HTML | 站序、站名、去/返程 → `stop.jsp?sid=` |
| `stop.jsp?sid=<n>` | **302 →** `stoplocation.jsp?slid=<n>` | **sid → slid 站位映射** |
| `RouteDyna?routeid=<n>` | **JSON** | 整條路線 ETA + **公車即時經緯度** |
| **`StopLocationDyna?stoplocationid=<slid>`** | **JSON** | **單一站位所有路線 ETA ← 即時路徑主力** |

> **`slid`（站位）是本專案的核心鍵**：它把同一側馬路的多條路線站牌聚合成一個實體點。
> 使用者站在那裡，一次呼叫即取得該站位**所有路線**的 ETA，不需逐路線查詢。

### 1.2 `n1` 欄位解碼（實測驗證）

```
N1, 19111,  15111,  222240644,  19105,  187026, 0, 716,   5,    0,    2, 260817142228, ...
     stopId  routeId 車輛ID或     前一站   —      —  ETA秒  剩餘   狀態   —   timestamp
                     "14:50"發車時刻                        站數
```

| 索引 | 意義 | 備註 |
|---|---|---|
| 1 | `stopId` (sid) | |
| 2 | `routeId` (rid) | |
| 3 | 車輛 ID 或 `HH:MM` | 尚未發車時給預定發車時刻 |
| **7** | **預估到站秒數** | **`-1` = 無班次 / 末班已過** |
| 8 | 剩餘站數 | |
| 9 | 狀態碼 | `2` = 未發車 |

### 1.3 `Bus[].a1` 欄位（公車即時位置）

```
A1, 410, 222238075, 1, 0, 104170, 1, 121.556790, 25.041457, 40,  271, ...
                                       lon         lat       速度  方位角
```

### 1.4 解析注意事項

| 事項 | 處理方式 |
|---|---|
| 頁面為 **Big5** 編碼 | 但**站名是 HTML numeric entity**（`&#x8606;&#x6d32;` = 蘆洲）→ 站名只需 entity decode，**不必處理 Big5**；僅公告文字需轉碼 |
| `UpdateTime` 含 entity | `14&#x3a;23&#x3a;58` → decode 後才是 `14:23:58` |
| **無 SLA、無版本保證** | parser 必須容錯：欄位數不足、型別異常時**降級回傳部分結果，不 panic** |
| 禮貌爬取 | 限流 2 req/s，設 `User-Agent`，失敗指數退避 |

---

## 2. 站位座標索引（離線建表）

`pda5284` **不含經緯度**，但「最近站位」需要站位座標才算得出距離。

**關鍵切分：站位位置是靜態資料 → 離線建表；ETA 是動態資料 → 即時查詢。**
不可把靜態資料放進即時路徑，那是純粹的延遲浪費。

### 2.1 建表 Job（Cloud Run Job + Cloud Scheduler，每日一次）

```
routelist.jsp
   └─► 全部 rid
        └─► route.jsp?rid=<n>   → sid + 站名 + 方向 + 站序
             └─► stop.jsp?sid=<n> (302) → slid
                  └─► 座標填充（見 2.2）
                       └─► stops.json → Cloud Storage
```

服務啟動時載入記憶體，建空間索引（geohash 或 S2 cell），查詢 ~1ms。

> ⚠️ **全量爬取約 400 條路線 × ~40 站 ≈ 1.6 萬次請求**，限流 2 req/s 需約 2 小時。
> Demo 階段先爬示範區域的路線即可，不必全台北。

### 2.2 座標來源（兩擇一，可互補）

| 方案 | 做法 | 優點 | 缺點 |
|---|---|---|---|
| **A. 公車 GPS 反推**（推薦） | `RouteDyna` 的 `Bus[].a1` 有即時經緯度 + 速度，`a2` 有目前所在 sid。取**速度 < 5 km/h**（真正停靠中）的樣本，按 sid 分群取**中位數** | 自足於同一資料源、無需 TWD97 轉換、精度 ±20m | 需累積樣本（跑數小時） |
| B. data.taipei 靜態資料 | [臺北市公車站牌位置圖](https://data.taipei/dataset/detail?id=48aa5bca-2a4f-4fb7-a658-43cba51d5d56) | 立即可用 | TWD97 需轉 WGS84；ID 與 pda5284 不通用，須靠站名 join |

> 建議：先用 B 快速起步，再以 A 持續校正。

---

## 3. 多 Agent 平行架構

「平行化」指**多個 agent 同時做不同的事**，由 orchestrator 分派與匯總。

### 3.1 Agent DAG

```
                        ┌──────────────────┐
                        │   Orchestrator   │
                        │  分派 / 匯流 / 決策 │
                        └────────┬─────────┘
      ┌──────────────────┬───────┴────────┬──────────────────┐
      ▼                  ▼                ▼                  │
┌───────────┐     ┌───────────┐    ┌───────────┐             │  Wave 1
│  Vision   │     │    Geo    │    │  Journey  │             │  真平行
│   Agent   │     │   Agent   │    │   Agent   │             │  互不依賴
│ 讀照片消歧義│     │ GPS→候選站位│    │ 目的地→路線 │             │
│  ★ LLM    │     │  純計算    │    │  ★ LLM    │             │
└─────┬─────┘     └─────┬─────┘    └─────┬─────┘             │
      └─────────┬───────┘                │                   │
                ▼  Orchestrator 決定 slid │                   │
         ┌─────────────┐                 │                   │  Wave 2
         │   Transit   │                 │                   │
         │    Agent    │                 │                   │
         │ slid→各線ETA │                 │                   │
         │   純查詢     │                 │                   │
         └──────┬──────┘                 │                   │
                └────────────┬───────────┘                   │
                             ▼                               │  Wave 3
                    ┌─────────────────┐                      │
                    │ Narrator Agent  │                      │
                    │ 組語音 + urgency │                      │
                    │     ★ LLM       │                      │
                    └─────────────────┘                      │
```

### 3.2 Agent 職責

| Agent | 需要 LLM？ | 輸入 | 輸出 |
|---|---|---|---|
| **Vision** | ✅ Gemini | 影格圖片 | 站名文字、路線號碼、「往○○」方向 |
| **Geo** | ❌ 純計算 | GPS 座標 | 候選站位 + 距離（由本地空間索引） |
| **Transit** | ❌ 純查詢 | `slid` | 各路線 ETA（`StopLocationDyna`） |
| **Journey** | ✅ Gemini | 目的地、Profile | `watch_list`（該搭哪幾條） |
| **Narrator** | ✅ Gemini | 上游全部 + Profile | `voice_summary`、`urgency` |

> **重點：不是每個 agent 都該用 LLM。** Geo 與 Transit 是確定性計算與查詢，
> 套上 LLM 只會變慢、變貴、變不可靠。**確定性節點仍然是 DAG 中的 agent。**

### 3.3 Go 實作骨架

```go
// internal/agent/orchestrator.go
type Agent interface {
	Name() string
	Run(ctx context.Context, in Blackboard) (Contribution, error)
}

// Blackboard：agent 之間唯一的共享狀態，每個 wave 結束後合併
type Blackboard struct {
	GPS      Location
	Image    []byte
	Profile  Profile
	Session  SessionState

	VisionOut  *VisionResult   // Wave 1 產出
	GeoOut     *GeoResult
	JourneyOut *JourneyResult
	SlID       int             // Orchestrator 於 Wave 1→2 之間決定
	TransitOut *TransitResult  // Wave 2 產出
}

func (o *Orchestrator) runWave(ctx context.Context, bb *Blackboard, agents []Agent) error {
	contribs := make([]Contribution, len(agents))
	g, gctx := errgroup.WithContext(ctx)
	for i, a := range agents {
		i, a := i, a
		g.Go(func() error {
			actx, cancel := context.WithTimeout(gctx, a.Budget())
			defer cancel()
			c, err := a.Run(actx, *bb)
			if err != nil {
				// 單一 agent 失敗不中斷整波 —— 記錄後降級
				log.Warn("agent failed", "agent", a.Name(), "err", err)
				contribs[i] = Contribution{Agent: a.Name(), Degraded: true}
				return nil
			}
			contribs[i] = c
			return nil
		})
	}
	_ = g.Wait()
	return bb.Merge(contribs)
}
```

> **設計要點：Wave 內單一 agent 失敗不取消其他 agent。**
> 例如照片模糊導致 Vision 失敗，Geo 仍應回傳最近站位 —— 服務**降級**而非**中斷**。
> 這與一般後端 `errgroup` 遇錯即取消的語意相反，是 agent 系統的必要調整。

---

## 4. 最近站位判定與距離門檻

### 4.1 判定流程

1. **Geo Agent**：手機 GPS → 空間索引查半徑 150m 內站位，依 haversine 距離排序
2. **消歧義**：若前幾名距離接近（差距 < 20m，通常是對向站牌同名），
   交由 **Vision Agent** 讀照片上的「往 ○○」決定哪一側；
   照片無站牌資訊時，**直接取最近者**
3. **門檻裁決**：

| 距離 | 行為 |
|---|---|
| ≤ 50m | 鎖定站位，正常回報 |
| 50–150m | 鎖定但降低信心，播報措辭改為「附近的 ○○ 站」 |
| **> 150m** | **回 `status: "not_found"`，`message: "尚未偵測到站牌"`** |

> 台北市區 GPS 誤差約 10–30m，50m 門檻可涵蓋定位漂移而不誤判到隔壁站。

### 4.2 站位鎖定狀態機（省成本與延遲）

```
IDLE ──► LOCATING ──► STOP_LOCKED ──► BOARDING ──► ON_BUS ──► ARRIVED
         (跑完整 DAG)   (只跑 Transit Agent，零 LLM)
```

鎖定後**不再呼叫 Vision / Journey**，僅輪詢 `StopLocationDyna`。
僅在下列條件重跑完整 DAG：

| 條件 | 理由 |
|---|---|
| 尚未鎖定 | 必要 |
| GPS 位移 > 30m | 可能換站 |
| 距上次辨識 > 60s | 防呆 |

> 一趟等車 5 分鐘 ≈ 150 幀，實際只需 2–3 次完整 DAG 執行。

---

## 5. 雙軌路徑

「即時性」與「長任務 agent」在同一條 request path 上互斥（LLM 多輪推理需 5–15s）。

```
┌─ Fast Path（每 1–2 秒，零 LLM）───────────────────────┐
│  POST /api/analyze                                    │
│  已鎖定 → 只跑 Transit Agent → 排序 → 回傳            │
│                                     目標 p95 < 800ms  │
└───────────────────────────────────────────────────────┘
              ↑ 讀                         ↓ 觸發
        ┌────────────────────────────────────────┐
        │   Session State (Firestore + Redis)    │
        │   locked_slid / watch_list / phase     │
        └────────────────────────────────────────┘
              ↑ 寫
┌─ Agent Path（非同步，Cloud Tasks 觸發）───────────────┐
│  完整 DAG：Vision ∥ Geo ∥ Journey → Transit → Narrator │
└───────────────────────────────────────────────────────┘
```

Agent Path 慢不影響服務 —— Fast Path 沿用上一輪的 `locked_slid` 與 `watch_list`，
體驗是**降級**而非**中斷**。

---

## 6. Skill 抽象

### 6.1 介面

```go
// internal/skill/skill.go
type Skill interface {
	Name() string
	Description() string
	InputSchema() map[string]any // JSON Schema，直接餵給 Gemini FunctionDeclaration
	Invoke(ctx context.Context, raw json.RawMessage) (any, error)
}
```

### 6.2 雙層設計：同一份實作供兩條路徑共用

```go
// internal/skill/stop_eta.go
type StopETA struct{ pda *pda.Client }

// typed：Fast Path / 確定性 agent 直接呼叫，編譯期型別安全、零 JSON 開銷
func (s *StopETA) Do(ctx context.Context, in StopETAIn) (StopETAOut, error) {
	return s.pda.StopLocationDyna(ctx, in.SlID)
}

// untyped：LLM agent 走 function calling 這條
func (s *StopETA) Invoke(ctx context.Context, raw json.RawMessage) (any, error) {
	var in StopETAIn
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, fmt.Errorf("skill %s: bad args: %w", s.Name(), err)
	}
	return s.Do(ctx, in)
}
```

> 避免「agent 查到的資料與 API 回傳不一致」這類難查的 bug。

### 6.3 Skill 清單

| Skill | 底層 | 使用者 |
|---|---|---|
| `nearest_stops` | 本地空間索引 | Geo Agent |
| `stop_eta` | `StopLocationDyna` | Transit Agent |
| `route_eta` | `RouteDyna` | Transit / Journey Agent |
| `route_stops` | `route.jsp`（快取） | Journey Agent |
| `vision_read_sign` | Vertex AI Gemini | Vision Agent |
| `plan_transit` | Routes API (`TRANSIT`) | Journey Agent |
| `place_lookup` | Places API | Journey Agent |
| `tts` | Cloud Text-to-Speech | Narrator Agent |
| ~~`notify_driver`~~ | — | **本階段不實作** |

### 6.4 Registry：統一 timeout / retry / log / metrics

```go
func (r *Registry) InvokeParallel(ctx context.Context, calls []Call) []Result {
	res := make([]Result, len(calls))
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for i, c := range calls {
		i, c := i, c
		g.Go(func() error {
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			v, err := r.skills[c.Name].Invoke(cctx, c.Args)
			res[i] = Result{Name: c.Name, Value: v, Err: err} // 錯誤回給 LLM，不中斷整輪
			return nil
		})
	}
	_ = g.Wait()
	return res
}
```

> tool 執行失敗是 agent 迴圈的**正常輸入**（LLM 讀到錯誤會改參數重試），
> 不該讓 `errgroup` 取消其他平行 tool。

---

## 7. GCP API 盤點

### 7.1 「無 GPU / TPU」的實際影響

| | 受影響？ | 說明 |
|---|---|---|
| Vertex AI Gemini / Vision / TTS / STT | **否** | Managed inference API，運算在 Google 端，只付呼叫費 |
| Cloud Run 服務本體 | **否** | 只做 HTTP 呼叫與 JSON 處理，CPU-only 足夠 |
| Vertex AI Custom Training | **是** | 需 accelerator → **不採用** |
| GKE GPU node / GCE GPU | **是** | 自架模型 → **不採用** |

> **結論**：全系統走 managed API，不自訓練任何模型。

### 7.2 一次開通

```bash
gcloud services enable \
  run.googleapis.com cloudbuild.googleapis.com artifactregistry.googleapis.com \
  aiplatform.googleapis.com texttospeech.googleapis.com speech.googleapis.com \
  firestore.googleapis.com storage.googleapis.com redis.googleapis.com \
  cloudtasks.googleapis.com cloudscheduler.googleapis.com \
  secretmanager.googleapis.com identitytoolkit.googleapis.com \
  logging.googleapis.com monitoring.googleapis.com cloudtrace.googleapis.com \
  routes.googleapis.com places.googleapis.com geolocation.googleapis.com

gcloud services list --enabled   # 驗證
```

### 7.3 服務對應

| 職責 | 服務 | 備註 |
|---|---|---|
| API 服務 | **Cloud Run** | `min-instances=1`，避免冷啟動吃掉即時性 |
| 建表 Job | **Cloud Run Job** + **Cloud Scheduler** | 每日爬 pda5284 建站位索引 |
| Vision / Journey / Narrator Agent | **Vertex AI Gemini** | `aiplatform.googleapis.com` |
| 語音合成 | **Cloud Text-to-Speech** | Chirp3-HD 中文 |
| 語音輸入目的地 | **Cloud Speech-to-Text** | 「我要去台北車站」 |
| Profile / Session | **Firestore** | |
| ETA 熱快取 | **Memorystore Redis** | TTL 15s，擋住 pda5284 流量 |
| 站位索引 / 音檔 | **Cloud Storage** | |
| Agent Path 觸發 | **Cloud Tasks** | 內建重試 |
| 行程規劃 | **Routes API** | `travelMode=TRANSIT` |
| GPS 校正 | **Geolocation API** | 都市峽谷 WiFi 輔助定位 |
| 密鑰 | **Secret Manager** | |
| **延遲追蹤** | **Cloud Trace** | **p95 < 800ms 目標的必要工具** |

---

## 8. 專案結構

```
backend/
├─ cmd/
│  ├─ server/main.go          # Cloud Run 服務
│  └─ indexer/main.go         # Cloud Run Job：爬 pda5284 建站位索引
├─ internal/
│  ├─ httpapi/                # gin handler + DTO
│  │  ├─ analyze.go
│  │  └─ profile.go
│  ├─ agent/                  # ★ 多 agent 編排
│  │  ├─ orchestrator.go      # DAG + wave 平行執行
│  │  ├─ blackboard.go
│  │  ├─ vision.go / geo.go / transit.go / journey.go / narrator.go
│  │  └─ prompts/
│  ├─ skill/                  # ★ Skill interface + Registry + 各 skill
│  ├─ pda/                    # ★ pda5284 client + parser
│  │  ├─ client.go            # 限流、重試、快取
│  │  ├─ dyna.go              # RouteDyna / StopLocationDyna JSON 解碼
│  │  └─ scrape.go            # routelist / route / stop 頁面解析
│  ├─ stopindex/              # 站位空間索引（geohash/S2）+ haversine
│  ├─ session/                # 狀態機 + Firestore/Redis
│  ├─ vertex/                 # Gemini client
│  └─ config/
└─ deploy/                    # Dockerfile + Cloud Run yaml
```

---

## 9. API 契約補充建議

`api.md` 已更新為「使用者 GPS 位置等資訊 + 影格圖片」。建議明確化：

```jsonc
// POST /api/analyze
{
  "session_id": "...",                      // 讓後端維持站位鎖定狀態機
  "location": { "lat": 25.014, "lng": 121.534, "accuracy_m": 8 },
  "image": "<base64 或 GCS URI>"            // 選填：無圖片時純靠 GPS 取最近站位
}
// user_id 走 header（與 /api/profile 一致）
```

### `voice_summary` 建議改為以文字為主

```jsonc
"voice_summary": {
  "text": "307路還有3分鐘進站，往台北車站",
  "audio_url": "https://storage.googleapis.com/..."  // 選用後備
}
```

> 視障使用者通常已在系統層設定慣用語速（常見 2–3x）與音色。強制播 server 端音檔會
> **破壞既有無障礙體驗**，且多一次網路來回。前端應優先以裝置 TTS 唸 `text`。

### `urgency` 不應只看 `eta_minutes`

```go
func urgency(etaMin int, inWatchList bool, p Profile) string {
	if !inWatchList {
		return "low" // 不是他要搭的車，再快也不該喊 high
	}
	budget := p.ReactionBufferMin // tunnel_vision 需要更長的移動時間
	switch {
	case etaMin <= budget:
		return "high"
	case etaMin <= budget+5:
		return "medium"
	default:
		return "low"
	}
}
```

> 一個站位常有 10 條以上路線，若不以 `inWatchList` 過濾，
> 使用者會被無關的車不斷打斷，系統反而成為干擾源。

---

## 10. 風險與待確認

| # | 風險 | 影響 | 對策 |
|---|---|---|---|
| 1 | **pda5284 無 SLA、格式可能變動** | 資料層全掛 | parser 全面容錯；欄位缺失降級不 panic；加合成監控 |
| 2 | **站位座標需自行建表** | 阻擋「最近站位」功能 | §2.2 方案 B 快速起步，方案 A 持續校正 |
| 3 | 全量爬取 ~1.6 萬請求 / 2 小時 | 建表耗時、可能被限流 | 限流 2 req/s；Demo 先爬示範區域 |
| 4 | 對向站牌同名 | 播報錯方向 | §4.1 Vision Agent 消歧義 |
| 5 | 都市峽谷 GPS 漂移 | 站位配對錯誤 | Geolocation API 校正 + 50m 門檻 |
| 6 | `go.mod` module 名稱錯誤 | **編譯必失敗** | 見 §11 P0 |

---

## 11. 分階段實作

| 階段 | 內容 | 完成後可 demo |
|---|---|---|
| **P0**（~30 分） | 修 `go.mod`、建目錄骨架、endpoint 回 mock、`Skill` interface + Registry、Dockerfile | **前端可立即並行開工** |
| **P1**（2–3 h） | `pda` client + parser（`StopLocationDyna` / `RouteDyna`）+ indexer 爬示範路線建站位索引 | 給 `slid` → 回真實 ETA |
| **P2**（2–3 h） | Geo Agent（空間索引 + 距離門檻）+ Fast Path 串接 + Firestore profile | **GPS → 最近站位 → 真實到站時間；太遠回 `not_found`** |
| **P3** | Orchestrator + Vision / Journey / Narrator Agent 平行 DAG + Cloud Tasks + TTS | 需求 §0.4–0.6 完成 |
| **P4** | 站位鎖定狀態機、urgency 調校、`safe_zone` / `font_scale`、Cloud Trace 優化 | 體驗打磨 |

**排序理由**：P2 結束就有完整可 demo 的故事（站在站牌旁 → 唸出下一班車），
之後每階段都是加值而非重寫。最怕先做多 agent，demo 時什麼都跑不動。

### P0 立即要修的 bug

```go
// go.mod 第 1 行 —— 目前宣告「本模組就是 gin」
module github.com/gin-gonic/gin
```

`main.go` 的 `import "github.com/gin-gonic/gin"` 會解析回自己 → **編譯必失敗**。
且尚無 `go.sum`，gin 未實際下載。

```bash
module github.com/him6794/devjam
go mod tidy
```
