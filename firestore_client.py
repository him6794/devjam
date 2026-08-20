import os
from datetime import datetime, timedelta, timezone

from google.cloud import firestore


GCP_PROJECT_ID = os.environ.get("GCP_PROJECT_ID", "devjam26aug17tpe-1280")

_db = None


def get_db() -> firestore.Client:
    global _db
    if _db is None:
        _db = firestore.Client(project=GCP_PROJECT_ID)
    return _db




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




ALERT_TTL_MINUTES = 10  


def create_bus_alert(route: str, stop_name: str, impairment_type: str) -> None:
    get_db().collection("bus_alerts").add({
        "route": route,
        "stop_name": stop_name,
        "impairment_type": impairment_type,
        "created_at": firestore.SERVER_TIMESTAMP,
    })


def get_active_alerts(route: str) -> list[dict]:
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
