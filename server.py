"""
城市之眼 Demo 伺服器：
- 拍照掃描 -> Gemini 摘要 -> TTS 語音
- 使用者偏好（校準一次，Firestore 記住）
- 通知司機（Firestore 寫入警示，司機端輪詢）
"""
import os
import re
import uuid

from flask import Flask, request, jsonify, send_from_directory, render_template_string

from pipeline import run_pipeline
import firestore_client as db

BASE_DIR = os.path.dirname(os.path.abspath(__file__))
UPLOAD_DIR = os.path.join(BASE_DIR, "uploads")
AUDIO_DIR = os.path.join(BASE_DIR, "output")
os.makedirs(UPLOAD_DIR, exist_ok=True)
os.makedirs(AUDIO_DIR, exist_ok=True)

app = Flask(__name__)

PASSENGER_HTML = """
<!doctype html>
<html lang="zh-Hant">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
  <title>城市之眼</title>
  <style>
    html, body {
      margin: 0; padding: 0; height: 100%; overflow: hidden;
      background: #000; font-family: sans-serif; color: #fff;
    }
    #camera {
      position: fixed; inset: 0; width: 100%; height: 100%;
      object-fit: cover; background: #000;
    }
    #tapLayer { position: fixed; inset: 0; z-index: 5; }
    #status {
      position: fixed; top: 0; left: 0; right: 0; z-index: 10;
      padding: 16px; text-align: center; font-size: 1.1rem;
      background: rgba(0,0,0,0.55);
    }
    #summaryBox {
      position: fixed; bottom: 90px; left: 0; right: 0; z-index: 10;
      padding: 20px 20px; background: rgba(0,0,0,0.75);
      font-size: 1.6rem; line-height: 1.5; min-height: 60px;
      display: none;
    }
    #notifyBtn {
      position: fixed; bottom: 0; left: 0; right: 0; z-index: 20;
      padding: 20px; font-size: 1.3rem; font-weight: bold;
      background: #d32f2f; color: #fff; border: none;
      display: none;
    }
    #hint {
      position: fixed; bottom: 40%; left: 0; right: 0; z-index: 10;
      text-align: center; font-size: 1.2rem; color: rgba(255,255,255,0.85);
      pointer-events: none;
    }
    #calibration {
      position: fixed; inset: 0; z-index: 50; background: #111;
      display: flex; flex-direction: column; align-items: center; justify-content: center;
      padding: 24px; text-align: center;
    }
    #calibration h2 { font-size: 1.4rem; margin-bottom: 24px; }
    .calBtn {
      width: 100%; max-width: 360px; margin: 8px 0; padding: 18px;
      font-size: 1.2rem; border-radius: 10px; border: 2px solid #666;
      background: #222; color: #fff;
    }
    .calBtn:active { background: #444; }
  </style>
</head>
<body>
  <div id="calibration">
    <h2>第一次使用，請選擇你的視覺狀況</h2>
    <button class="calBtn" data-type="隧道視野">隧道視野</button>
    <button class="calBtn" data-type="中心黑點">中心黑點</button>
    <button class="calBtn" data-type="低視力">低視力</button>
    <button class="calBtn" data-type="全盲">全盲</button>
  </div>

  <video id="camera" autoplay playsinline muted></video>
  <canvas id="canvas" style="display:none;"></canvas>

  <div id="status">城市之眼 — 點螢幕任意處掃描</div>
  <div id="hint">點一下畫面開始掃描</div>
  <div id="summaryBox"></div>
  <button id="notifyBtn">通知司機：本班車有視障乘客等車</button>
  <div id="tapLayer"></div>
  <audio id="player" style="display:none;"></audio>

  <script>
    const video = document.getElementById('camera');
    const canvas = document.getElementById('canvas');
    const statusEl = document.getElementById('status');
    const hintEl = document.getElementById('hint');
    const summaryBox = document.getElementById('summaryBox');
    const player = document.getElementById('player');
    const tapLayer = document.getElementById('tapLayer');
    const calibration = document.getElementById('calibration');
    const notifyBtn = document.getElementById('notifyBtn');

    let busy = false;
    let lastRoute = null;

    function getUserId() {
      let id = localStorage.getItem('user_id');
      if (!id) {
        id = 'u_' + Math.random().toString(36).slice(2, 10);
        localStorage.setItem('user_id', id);
      }
      return id;
    }
    const userId = getUserId();

    async function checkProfile() {
      const res = await fetch('/api/profile?user_id=' + userId);
      const data = await res.json();
      if (data.exists) {
        calibration.style.display = 'none';
        initCamera();
      } else {
        calibration.style.display = 'flex';
      }
    }

    document.querySelectorAll('.calBtn').forEach(btn => {
      btn.addEventListener('click', async () => {
        const impairmentType = btn.dataset.type;
        await fetch('/api/profile', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            user_id: userId,
            impairment_type: impairmentType,
            safe_zone: impairmentType === '中心黑點' ? 'top' : 'center',
            font_size: 'large',
            voice_enabled: true,
          }),
        });
        calibration.style.display = 'none';
        initCamera();
      });
    });

    async function initCamera() {
      try {
        const stream = await navigator.mediaDevices.getUserMedia({
          video: { facingMode: 'environment' }
        });
        video.srcObject = stream;
      } catch (err) {
        statusEl.innerText = '無法開啟相機：' + err.message + '（需要 HTTPS 或 localhost）';
      }
    }

    async function captureAndAnalyze() {
      if (busy) return;
      busy = true;
      if (navigator.vibrate) navigator.vibrate(80);
      hintEl.style.display = 'none';
      statusEl.innerText = '分析中...';
      notifyBtn.style.display = 'none';

      canvas.width = video.videoWidth;
      canvas.height = video.videoHeight;
      canvas.getContext('2d').drawImage(video, 0, 0);

      canvas.toBlob(async (blob) => {
        try {
          const formData = new FormData();
          formData.append('image', blob, 'scan.jpg');
          formData.append('user_id', userId);
          const res = await fetch('/api/analyze', { method: 'POST', body: formData });
          const data = await res.json();

          if (data.status !== 'success') {
            statusEl.innerText = '失敗: ' + data.message;
          } else {
            statusEl.innerText = '點螢幕再次掃描';
            summaryBox.innerText = data.summary;
            summaryBox.style.display = 'block';
            player.src = data.audio_url;
            player.play();
            lastRoute = data.route;
            if (lastRoute) {
              notifyBtn.style.display = 'block';
              notifyBtn.innerText = '通知司機：' + lastRoute + ' 號公車有視障乘客等車';
            }
          }
        } catch (err) {
          statusEl.innerText = '發生錯誤: ' + err.message;
        } finally {
          busy = false;
        }
      }, 'image/jpeg', 0.9);
    }

    notifyBtn.addEventListener('click', async (e) => {
      e.stopPropagation();
      if (!lastRoute) return;
      notifyBtn.innerText = '通知中...';
      await fetch('/api/notify_driver', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ user_id: userId, route: lastRoute, stop_name: '' }),
      });
      notifyBtn.innerText = '已通知司機';
      if (navigator.vibrate) navigator.vibrate([50, 50, 50]);
    });

    tapLayer.addEventListener('click', captureAndAnalyze);
    checkProfile();
  </script>
</body>
</html>
"""

DRIVER_HTML = """
<!doctype html>
<html lang="zh-Hant">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>司機端 — 城市之眼</title>
  <style>
    body { font-family: sans-serif; max-width: 480px; margin: 24px auto; padding: 0 16px; }
    input, button { font-size: 1.1rem; padding: 10px; }
    #alerts { margin-top: 20px; }
    .alert {
      background: #fff3cd; border: 1px solid #d32f2f; border-left: 6px solid #d32f2f;
      padding: 14px; margin-bottom: 10px; border-radius: 6px; font-size: 1.1rem;
    }
  </style>
</head>
<body>
  <h2>司機端 — 路線警示</h2>
  <input id="routeInput" placeholder="輸入路線號碼，例如 262" />
  <button id="startBtn">開始監看</button>
  <div id="alerts"></div>

  <script>
    let route = null;
    let timer = null;

    async function poll() {
      if (!route) return;
      const res = await fetch('/api/driver_alerts?route=' + encodeURIComponent(route));
      const data = await res.json();
      const box = document.getElementById('alerts');
      if (!data.alerts || data.alerts.length === 0) {
        box.innerHTML = '<p>目前沒有警示</p>';
        return;
      }
      box.innerHTML = data.alerts.map(a =>
        `<div class="alert">⚠️ ${a.route} 號路線有視障乘客等車（${a.impairment_type || '未知類型'}）</div>`
      ).join('');
    }

    document.getElementById('startBtn').addEventListener('click', () => {
      route = document.getElementById('routeInput').value.trim();
      if (!route) return;
      if (timer) clearInterval(timer);
      poll();
      timer = setInterval(poll, 3000);
    });
  </script>
</body>
</html>
"""


@app.route("/")
def index():
    return render_template_string(PASSENGER_HTML)


@app.route("/driver")
def driver():
    return render_template_string(DRIVER_HTML)


@app.route("/api/profile", methods=["GET"])
def api_get_profile():
    user_id = request.args.get("user_id")
    if not user_id:
        return jsonify({"status": "error", "message": "缺少 user_id"}), 400
    profile = db.get_user_profile(user_id)
    if profile is None:
        return jsonify({"exists": False})
    return jsonify({"exists": True, "profile": profile})


@app.route("/api/profile", methods=["POST"])
def api_save_profile():
    data = request.get_json(force=True)
    db.save_user_profile(
        user_id=data["user_id"],
        impairment_type=data.get("impairment_type", ""),
        safe_zone=data.get("safe_zone", "center"),
        font_size=data.get("font_size", "large"),
        voice_enabled=data.get("voice_enabled", True),
    )
    return jsonify({"status": "success"})


@app.route("/api/analyze", methods=["POST"])
def api_analyze():
    file = request.files.get("image")
    if not file:
        return jsonify({"status": "error", "message": "缺少照片"}), 400

    job_id = str(uuid.uuid4())[:8]
    image_path = os.path.join(UPLOAD_DIR, f"{job_id}.jpg")
    file.save(image_path)

    audio_path = os.path.join(AUDIO_DIR, f"{job_id}.wav")

    try:
        result = run_pipeline(image_path, audio_path)
    except Exception as e:
        return jsonify({"status": "error", "message": str(e)}), 500

    route_match = re.search(r"\d{2,4}", result["summary"])
    route = route_match.group() if route_match else None

    return jsonify({
        "status": "success",
        "summary": result["summary"],
        "audio_url": f"/audio/{job_id}.wav",
        "route": route,
    })


@app.route("/api/notify_driver", methods=["POST"])
def api_notify_driver():
    data = request.get_json(force=True)
    user_id = data.get("user_id")
    route = data.get("route")
    if not route:
        return jsonify({"status": "error", "message": "缺少路線號碼"}), 400

    profile = db.get_user_profile(user_id) if user_id else None
    impairment_type = profile.get("impairment_type") if profile else None

    db.create_bus_alert(
        route=route,
        stop_name=data.get("stop_name", ""),
        impairment_type=impairment_type,
    )
    return jsonify({"status": "success"})


@app.route("/api/driver_alerts", methods=["GET"])
def api_driver_alerts():
    route = request.args.get("route")
    if not route:
        return jsonify({"status": "error", "message": "缺少 route"}), 400
    alerts = db.get_active_alerts(route)
    return jsonify({"status": "success", "alerts": alerts})


@app.route("/audio/<path:filename>")
def serve_audio(filename):
    return send_from_directory(AUDIO_DIR, filename)


if __name__ == "__main__":
    port = int(os.environ.get("PORT", 5001))
    debug = os.environ.get("PORT") is None  # Cloud Run 上關掉 debug/reloader
    app.run(host="0.0.0.0", port=port, debug=debug)
