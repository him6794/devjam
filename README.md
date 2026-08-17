# 城市之眼 — Docker Compose

## 啟動完整服務

```bash
docker compose up --build
```

開啟 <http://localhost:8080> 使用乘客端，或開啟
<http://localhost:8080/driver> 使用司機端。

服務分工：

- `gateway`：Nginx，提供同源入口並把 `/api/*` 代理到 Go API。
- `frontend`：Flask，只負責模板與靜態檔案。
- `api`：Go agent backend，提供分析、個人偏好、通知與警示 API。

未設定 Google Cloud credentials 時，API 仍可啟動；Gemini 視覺辨識與 TTS
會自動降級，GPS/站牌查詢與前端頁面仍可使用。
