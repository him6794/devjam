#!/usr/bin/env python3
import json
import subprocess
import time
import requests
import sys

BASE = "http://localhost:8080"

def test(name, method, path, **kwargs):
    try:
        r = requests.request(method, f"{BASE}{path}", timeout=10, **kwargs)
        ok = "✓" if 200 <= r.status_code < 300 else "✗"
        print(f"{ok} {name:40} HTTP {r.status_code}")
        if r.status_code >= 400:
            print(f"   {r.text[:100]}")
        return r
    except Exception as e:
        print(f"✗ {name:40} ERROR: {e}")
        return None

print("=" * 60)
print("全功能測試 (api.md § 1-4)")
print("=" * 60)

# 1. /api/analyze - GPS 定位
print("\n[1] /api/analyze - 有效座標(台北市)")
r = test("分析 - 有效座標",
    "POST", "/api/analyze",
    headers={"Content-Type": "application/json"},
    json={"location": {"lat": 25.051717, "lng": 121.552853}})
if r and r.status_code == 200:
    d = r.json()
    print(f"   status: {d.get('status')}")
    if d.get('status') == 'success':
        print(f"   station_name: {d.get('station_name')!r}")
        buses = d.get('buses', [])
        print(f"   buses: {len(buses)} 筆")
        if buses:
            b = buses[0]
            print(f"     [0] route={b.get('route')} eta={b.get('eta_minutes')} urgency={b.get('urgency')} direction={b.get('direction')}")
        voice = d.get('voice_summary', '')
        audio_url = d.get('voice_audio_url', '')
        print(f"   voice_summary: {voice!r}")
        print(f"   voice_audio_url: {'(public URL)' if audio_url.startswith('https://') else '(empty)'}")
        display = d.get('display', {})
        print(f"   display: safe_zone={display.get('safe_zone_position')} font={display.get('font_scale')}")

# 2. /api/analyze - 無效座標
print("\n[2] /api/analyze - 偏遠座標(南太平洋)")
r = test("分析 - 偏遠座標(not_found)",
    "POST", "/api/analyze",
    headers={"Content-Type": "application/json"},
    json={"location": {"lat": -40.0, "lng": 160.0}})
if r and r.status_code == 200:
    d = r.json()
    print(f"   status: {d.get('status')} (應為 not_found)")

# 3. /api/analyze - 缺地點
print("\n[3] /api/analyze - 缺少 location")
r = test("分析 - 缺 location",
    "POST", "/api/analyze",
    headers={"Content-Type": "application/json"},
    json={})
print(f"   (應為 400: {r.status_code if r else 'N/A'})")

# 4. /api/profile GET
print("\n[4] /api/profile - GET")
uid = f"testuser_{int(time.time())}"
test(f"個人偏好 GET - 未設定",
    "GET", "/api/profile",
    headers={"X-User-Id": uid})

# 5. /api/profile POST
print("\n[5] /api/profile - POST")
r = test(f"個人偏好 SET",
    "POST", "/api/profile",
    headers={"X-User-Id": uid, "Content-Type": "application/json"},
    json={
        "impairment_type": "tunnel_vision",
        "safe_zone": {"x": 50, "y": 25, "radius": 40},
        "font_scale": 1.8,
        "voice_enabled": True
    })
if r and r.status_code == 200:
    d = r.json()
    print(f"   impairment: {d.get('impairment_type')}")
    print(f"   safe_zone: {d.get('safe_zone')}")

# 6. /api/profile GET again
print("\n[6] /api/profile - GET (驗證已存)")
r = test(f"個人偏好 GET - 已設定",
    "GET", "/api/profile",
    headers={"X-User-Id": uid})
if r and r.status_code == 200:
    d = r.json()
    print(f"   exists: {d.get('exists')} (應為 true)")
    print(f"   font_scale: {d.get('font_scale')} (應為 1.8)")

# 7. /api/analyze with profile
print("\n[7] /api/analyze - 搭配個人偏好")
r = test("分析 + 個人偏好(display 應用 font=1.8, safe_zone=top)",
    "POST", "/api/analyze",
    headers={"Content-Type": "application/json", "X-User-Id": uid},
    json={"location": {"lat": 25.051717, "lng": 121.552853}})
if r and r.status_code == 200:
    d = r.json()
    display = d.get('display', {})
    print(f"   display: safe_zone={display.get('safe_zone_position')} (應為 top) font={display.get('font_scale')} (應為 1.8)")

# 8. /api/notify_driver
print("\n[8] /api/notify_driver - 建立告警")
r = test("通知司機",
    "POST", "/api/notify_driver",
    headers={"Content-Type": "application/json"},
    json={"route": "307", "station_name": "捷運公館站", "impairment_type": "tunnel_vision"})
if r and r.status_code == 200:
    print(f"   status: {r.json().get('status')} alert_id: {r.json().get('alert_id')}")

# 9. /api/driver_alerts
print("\n[9] /api/driver_alerts - 查詢路線告警")
r = test("司機告警查詢", "GET", "/api/driver_alerts?route=307")
if r and r.status_code == 200:
    print(f"   alerts: {len(r.json().get('alerts', []))} 筆")

# 10. /api/skills
print("\n[10] /api/skills - 列舉")
r = test("列舉所有 skill",
    "GET", "/api/skills")
if r and r.status_code == 200:
    d = r.json()
    skills = d.get('skills', [])
    print(f"   count: {len(skills)}")
    for s in skills:
        print(f"     - {s.get('name')}: {s.get('description')[:50]}")

# 11. /healthz
print("\n[11] /healthz")
test("健康檢查", "GET", "/healthz")

print("\n" + "=" * 60)
print("測試完成")
print("=" * 60)
