"""
Firestore 存取層：使用者偏好設定 + 公車司機警示。
用 Application Default Credentials 驗證（本機用 gcloud 登入的帳號；
Cloud Run 上會自動用該服務的內建身分，不用額外設定）。
"""
import os
from datetime import datetime, timedelta, timezone

from google.cloud import firestore

# 主辦方提供的黑客松專案：DevJam 2026 GDGoC-1280
GCP_PROJECT_ID = os.environ.get("GCP_PROJECT_ID", "devjam26aug17tpe-1280")

_db = None


def get_db() -> firestore.Client:
    global _db
    if _db is None:
        _db = firestore.Client(project=GCP_PROJECT_ID)
    return _db


# ========== 使用者偏好（校準一次，之後自動讀取） ==========

def get_user_profile(user_id: str) -> dict | None:
    doc = get_db().collection("users").document(user_id).get()
    return doc.to_dict() if doc.exists else None


def save_user_profile(user_id: str, impairment_type: str, visible_radius_percent: int,
                       font_size_px: int, theme: str, voice_enabled: bool) -> None:
    get_db().collection("users").document(user_id).set({
        "impairment_type": impairment_type,
        "visible_radius_percent": visible_radius_percent,
        "font_size_px": font_size_px,
        "theme": theme,
        "voice_enabled": voice_enabled,
        "updated_at": firestore.SERVER_TIMESTAMP,
    })


# ========== 公車司機警示 ==========

ALERT_TTL_MINUTES = 10  # 超過這個時間的警示視為過期，司機端不再顯示


def create_bus_alert(route: str, stop_name: str, impairment_type: str) -> None:
    get_db().collection("bus_alerts").add({
        "route": route,
        "stop_name": stop_name,
        "impairment_type": impairment_type,
        "created_at": firestore.SERVER_TIMESTAMP,
    })


def get_active_alerts(route: str) -> list[dict]:
    """只用單一 equality 篩選（不含 order_by/range），避免需要額外建立
    Firestore 複合索引——時間過濾與排序改在 Python 這邊做，demo 當下更穩。"""
    docs = get_db().collection("bus_alerts").where("route", "==", route).limit(20).stream()
    cutoff = datetime.now(timezone.utc) - timedelta(minutes=ALERT_TTL_MINUTES)

    alerts = []
    for d in docs:
        data = d.to_dict()
        created_at = data.get("created_at")
        if created_at and created_at >= cutoff:
            alerts.append(data)

    alerts.sort(key=lambda a: a.get("created_at"), reverse=True)
    return alerts
