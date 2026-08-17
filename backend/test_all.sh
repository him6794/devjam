#!/bin/bash
set -e

BASE="http://localhost:8080"

pass() { echo "✓ $1"; }
fail() { echo "✗ $1"; }

json_value() {
  python -c '
import json
import sys

value = json.load(sys.stdin)
for part in sys.argv[1].split("."):
    value = value.get(part) if isinstance(value, dict) else value
if value is None:
    print("")
elif isinstance(value, bool):
    print(str(value).lower())
else:
    print(value)
' "$1"
}

json_length() {
  python -c '
import json
import sys

value = json.load(sys.stdin)
for part in sys.argv[1].split("."):
    value = value.get(part) if isinstance(value, dict) else value
print(len(value))
' "$1"
}

echo "============================================================"
echo "全功能測試 (api.md § 1-4)"
echo "============================================================"

# 1. /api/analyze - 有效座標
echo ""
echo "[1] /api/analyze - 有效座標(台北市)"
RESP=$(curl -s -X POST "$BASE/api/analyze" \
  -H "Content-Type: application/json" \
  -d '{"location":{"lat":25.051717,"lng":121.552853}}')
STATUS=$(printf '%s' "$RESP" | json_value status)
if [ "$STATUS" = "success" ]; then
  pass "分析 - 有效座標"
  echo "$RESP"
else
  fail "分析 - 有效座標"
  echo "  回應: $RESP"
fi

# 2. /api/analyze - 無效座標(far away)
echo ""
echo "[2] /api/analyze - 偏遠座標"
RESP=$(curl -s -X POST "$BASE/api/analyze" \
  -H "Content-Type: application/json" \
  -d '{"location":{"lat":-40.0,"lng":160.0}}')
STATUS=$(printf '%s' "$RESP" | json_value status)
if [ "$STATUS" = "not_found" ]; then
  pass "分析 - not_found(應為 far away)"
else
  fail "分析 - 預期 not_found"
fi

# 3. /api/analyze - 缺 location
echo ""
echo "[3] /api/analyze - 缺 location"
HTTP=$(curl -s -w "%{http_code}" -o /dev/null -X POST "$BASE/api/analyze" \
  -H "Content-Type: application/json" \
  -d '{}')
if [ "$HTTP" = "400" ]; then
  pass "分析 - 400 Bad Request"
else
  fail "分析 - 預期 400，得 $HTTP"
fi

# 4. /api/profile GET (新用戶)
echo ""
echo "[4] /api/profile - GET (未設定)"
USER_ID="testuser_$(date +%s)"
RESP=$(curl -s -X GET "$BASE/api/profile" -H "X-User-Id: $USER_ID")
EXISTS=$(printf '%s' "$RESP" | json_value exists)
if [ "$EXISTS" = "false" ]; then
  pass "個人偏好 GET - 未設定(正常)"
else
  fail "個人偏好 GET - 預期 exists=false"
fi

# 5. /api/profile POST
echo ""
echo "[5] /api/profile - POST"
RESP=$(curl -s -X POST "$BASE/api/profile" \
  -H "X-User-Id: $USER_ID" \
  -H "Content-Type: application/json" \
  -d '{
    "impairment_type":"tunnel_vision",
    "safe_zone":{"x":50,"y":25,"radius":40},
    "font_scale":1.8,
    "voice_enabled":true
  }')
EXISTS=$(printf '%s' "$RESP" | json_value exists)
if [ "$EXISTS" = "true" ]; then
  pass "個人偏好 POST"
  echo "$RESP"
else
  fail "個人偏好 POST"
fi

# 6. /api/profile GET again
echo ""
echo "[6] /api/profile - GET (驗證已存)"
RESP=$(curl -s -X GET "$BASE/api/profile" -H "X-User-Id: $USER_ID")
EXISTS=$(printf '%s' "$RESP" | json_value exists)
FONT=$(printf '%s' "$RESP" | json_value font_scale)
if [ "$EXISTS" = "true" ] && [ "$FONT" = "1.8" ]; then
  pass "個人偏好 GET - 已設定，font_scale=1.8"
else
  fail "個人偏好 GET - 預期 exists=true font=1.8"
fi

# 7. /api/analyze 搭配個人偏好
echo ""
echo "[7] /api/analyze - 搭配個人偏好"
RESP=$(curl -s -X POST "$BASE/api/analyze" \
  -H "Content-Type: application/json" \
  -H "X-User-Id: $USER_ID" \
  -d '{"location":{"lat":25.051717,"lng":121.552853}}')
SAFE_ZONE=$(printf '%s' "$RESP" | json_value display.safe_zone_position)
FONT=$(printf '%s' "$RESP" | json_value display.font_scale)
if [ "$SAFE_ZONE" = "top" ] && [ "$FONT" = "1.8" ]; then
  pass "分析 + 個人偏好 - display 應用成功"
else
  fail "分析 + 個人偏好 - safe_zone=$SAFE_ZONE(預期 top) font=$FONT(預期 1.8)"
fi

# 8. /api/notify_driver
echo ""
echo "[8] /api/notify_driver - 建立告警"
HTTP=$(curl -s -w "%{http_code}" -o /dev/null -X POST "$BASE/api/notify_driver" \
  -H "Content-Type: application/json" \
  -d '{"route":"307","station_name":"捷運公館站","impairment_type":"tunnel_vision"}')
if [ "$HTTP" = "200" ]; then
  pass "通知司機 - 建立告警"
else
  fail "通知司機 - 預期 200，得 $HTTP"
fi

# 9. /api/driver_alerts
echo ""
echo "[9] /api/driver_alerts - 查詢路線告警"
RESP=$(curl -s -X GET "$BASE/api/driver_alerts?route=307")
COUNT=$(printf '%s' "$RESP" | json_length alerts)
if [ "$COUNT" -gt 0 ]; then
  pass "司機告警查詢 - $COUNT 筆"
else
  fail "司機告警查詢 - 預期至少 1 筆"
fi

# 10. /api/skills
echo ""
echo "[10] /api/skills"
RESP=$(curl -s -X GET "$BASE/api/skills")
COUNT=$(printf '%s' "$RESP" | json_length skills)
if [ "$COUNT" -gt 0 ]; then
  pass "列舉 skill - $COUNT 筆"
  echo "$RESP"
else
  fail "列舉 skill - 預期至少 1 筆"
fi

# 11. /healthz
echo ""
echo "[11] /healthz"
HTTP=$(curl -s -w "%{http_code}" -o /dev/null -X GET "$BASE/healthz")
if [ "$HTTP" = "200" ]; then
  pass "健康檢查"
else
  fail "健康檢查 - 預期 200，得 $HTTP"
fi

echo ""
echo "============================================================"
echo "測試完成"
echo "============================================================"
