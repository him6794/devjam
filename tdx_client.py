"""
TDX（交通部運輸資料流通服務）用戶端：即時公車到站資料。
用來縮小 AI Vision 的候選範圍——不是單純問「這是哪一號公車」，
而是先問「這個站牌現在理論上會出現哪些公車」，再讓 Vision 去確認。
"""
import os
import time

import requests

TDX_CLIENT_ID = os.environ.get("TDX_CLIENT_ID")
TDX_CLIENT_SECRET = os.environ.get("TDX_CLIENT_SECRET")
if not TDX_CLIENT_ID or not TDX_CLIENT_SECRET:
    from local_config import TDX_CLIENT_ID, TDX_CLIENT_SECRET  # 本機開發用，Cloud Run 上用環境變數

TOKEN_URL = "https://tdx.transportdata.tw/auth/realms/TDXConnect/protocol/openid-connect/token"
API_BASE = "https://tdx.transportdata.tw/api/basic/v2"

_token = None
_token_expiry = 0.0


def _get_token() -> str:
    global _token, _token_expiry
    if _token and time.time() < _token_expiry - 60:
        return _token

    resp = requests.post(
        TOKEN_URL,
        headers={"content-type": "application/x-www-form-urlencoded"},
        data={
            "grant_type": "client_credentials",
            "client_id": TDX_CLIENT_ID,
            "client_secret": TDX_CLIENT_SECRET,
        },
        timeout=10,
    )
    resp.raise_for_status()
    data = resp.json()
    _token = data["access_token"]
    _token_expiry = time.time() + data["expires_in"]
    return _token


def get_eta_candidates(city: str, route_name: str, stop_name_query: str = "") -> list[dict]:
    """查詢某路線在該城市的即時到站資料，依站名關鍵字篩選，依到站時間排序。"""
    token = _get_token()
    resp = requests.get(
        f"{API_BASE}/Bus/EstimatedTimeOfArrival/City/{city}/{route_name}",
        headers={"authorization": f"Bearer {token}"},
        params={"$format": "JSON"},
        timeout=10,
    )
    resp.raise_for_status()
    data = resp.json()
    if not isinstance(data, list):
        return []

    results = []
    for item in data:
        stop_name = (item.get("StopName") or {}).get("Zh_tw", "")
        if stop_name_query and stop_name_query not in stop_name:
            continue
        eta_seconds = item.get("EstimateTime")
        results.append({
            "route": (item.get("RouteName") or {}).get("Zh_tw", route_name),
            "stop_name": stop_name,
            "eta_seconds": eta_seconds,
            "eta_minutes": round(eta_seconds / 60) if eta_seconds is not None else None,
            "direction": item.get("Direction"),
            "stop_status": item.get("StopStatus"),  # 0=正常估計中, 其他=進站中/尚未發車等
        })

    results.sort(key=lambda r: (r["eta_seconds"] is None, r["eta_seconds"] or 0))
    return results


def build_vision_hint(candidates: list[dict], route_name: str) -> str | None:
    """把 TDX 查到的資料轉成給 Gemini Vision 的提示文字，縮小辨識範圍。"""
    if not candidates:
        return None
    top = candidates[0]
    if top["eta_minutes"] is None:
        return None
    return (
        f"根據臺北市公車即時動態資料（TDX），{route_name} 路公車預計約 "
        f"{top['eta_minutes']} 分鐘後到達使用者所在站牌。請特別確認畫面中出現的公車是否為 "
        f"{route_name} 號，並留意站牌上可能同時出現其他路線的公車。"
    )
