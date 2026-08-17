"""
城市之眼 Demo 伺服器：
- 拍照掃描 -> Gemini 摘要 -> TTS 語音
- 使用者偏好（校準一次，Firestore 記住）
- 通知司機（Firestore 寫入警示，司機端輪詢）
"""
import os
import re
import time
import uuid

from flask import Flask, request, jsonify, send_from_directory, render_template_string

from pipeline import run_pipeline
import firestore_client as db
import tdx_client

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
    #settingsBtn {
      position: fixed; top: 54px; right: 10px; z-index: 20;
      padding: 10px 16px; border-radius: 20px;
      background: #1976d2; color: #fff; border: none;
      font-size: 1rem; font-weight: bold;
      box-shadow: 0 2px 8px rgba(0,0,0,0.5);
    }
    #tripBar {
      position: fixed; top: 54px; left: 10px; z-index: 20;
      max-width: 55%; padding: 10px 14px; border-radius: 20px;
      background: rgba(0,0,0,0.6); color: #fff; border: 1px solid rgba(255,255,255,0.4);
      font-size: 0.9rem; text-align: left;
    }
    #hint {
      position: fixed; bottom: 40%; left: 0; right: 0; z-index: 10;
      text-align: center; font-size: 1.2rem; color: rgba(255,255,255,0.85);
      pointer-events: none;
    }

    /* ---- 校準精靈 ---- */
    #wizard {
      position: fixed; inset: 0; z-index: 50; background: #111;
      display: flex; flex-direction: column; align-items: center; justify-content: center;
      padding: 24px; text-align: center; overflow-y: auto;
    }
    #wizard h2 { font-size: 1.3rem; margin-bottom: 20px; }
    .wizStep { display: none; width: 100%; max-width: 420px; }
    .wizStep.active { display: block; }
    .calBtn {
      width: 100%; margin: 8px 0; padding: 18px;
      font-size: 1.2rem; border-radius: 10px; border: 2px solid #666;
      background: #222; color: #fff;
    }
    .calBtn:active { background: #444; }
    .fovPreviewWrap {
      position: relative; width: 100%; height: 260px; border-radius: 12px;
      overflow: hidden; margin-bottom: 16px; background: #000;
    }
    #fovVideo { width: 100%; height: 100%; object-fit: cover; }
    #fovMask {
      position: absolute; inset: 0; pointer-events: none;
      background: radial-gradient(circle at center, rgba(0,0,0,0) 0%, rgba(0,0,0,0) var(--r), rgba(0,0,0,0.92) var(--r));
    }
    input[type=range] { width: 100%; margin: 12px 0; }
    #fontPreview, #themePreview {
      border-radius: 10px; padding: 16px; margin: 12px 0; min-height: 50px;
    }
    .nextBtn {
      margin-top: 16px; width: 100%; padding: 16px; font-size: 1.2rem;
      background: #1976d2; color: #fff; border: none; border-radius: 10px;
    }

    /* ---- 全螢幕結果頁 ---- */
    #resultScreen {
      position: fixed; inset: 0; z-index: 40; display: none;
      flex-direction: column; align-items: center; justify-content: center;
      padding: 32px; text-align: center;
    }
    #resultText { line-height: 1.6; white-space: pre-wrap; }
    #notifyBtn {
      position: fixed; bottom: 0; left: 0; right: 0; z-index: 45;
      padding: 20px; font-size: 1.3rem; font-weight: bold;
      background: #d32f2f; color: #fff; border: none;
      display: none;
    }
    #feedbackBar {
      position: fixed; bottom: 66px; left: 0; right: 0; z-index: 45;
      display: none; gap: 8px; padding: 0 12px;
    }
    #feedbackBar button {
      flex: 1; padding: 12px 4px; font-size: 0.95rem; font-weight: bold;
      border: none; border-radius: 8px; color: #fff;
    }
    #fbBoarded { background: #2e7d32; }
    #fbMissed { background: #ef6c00; }
    #fbNotThis { background: #616161; }
  </style>
</head>
<body>
  <div id="wizard">
    <div class="wizStep active" id="step1">
      <h2>第一次使用，請選擇你的視覺狀況</h2>
      <button class="calBtn" data-type="隧道視野">隧道視野</button>
      <button class="calBtn" data-type="中心黑點">中心黑點</button>
      <button class="calBtn" data-type="低視力">低視力</button>
      <button class="calBtn" data-type="全盲">全盲</button>
    </div>

    <div class="wizStep" id="step2">
      <h2>調整滑桿，直到圓圈範圍接近你看得清楚的視野</h2>
      <div class="fovPreviewWrap">
        <video id="fovVideo" autoplay playsinline muted></video>
        <div id="fovMask" style="--r:50%;"></div>
      </div>
      <input type="range" id="fovSlider" min="10" max="100" value="60" />
      <div id="fovLabel">目前設定：可視範圍 60%</div>
      <button class="nextBtn" id="step2Next">下一步</button>
    </div>

    <div class="wizStep" id="step3">
      <h2>調整字體大小</h2>
      <div id="fontPreview">262 號公車，3 分鐘後到站</div>
      <input type="range" id="fontSlider" min="20" max="64" value="32" />
      <button class="nextBtn" id="step3Next">下一步</button>
    </div>

    <div class="wizStep" id="step4">
      <h2>選擇畫面色調</h2>
      <div id="themePreview">262 號公車，3 分鐘後到站</div>
      <button class="calBtn" data-theme="light">白底黑字</button>
      <button class="calBtn" data-theme="dark">黑底白字</button>
    </div>
  </div>

  <video id="camera" autoplay playsinline muted></video>
  <canvas id="canvas" style="display:none;"></canvas>

  <div id="status">城市之眼 — 點螢幕任意處掃描</div>
  <div id="hint">點一下畫面開始掃描</div>
  <div id="tapLayer"></div>
  <button id="settingsBtn" title="調整設定">⚙ 調整視野／字體</button>
  <button id="tripBar" title="設定要搭的公車路線與站牌">點擊設定要搭的公車路線與站牌</button>

  <div id="resultScreen">
    <div id="resultText"></div>
  </div>
  <button id="notifyBtn">通知司機：本班車有視障乘客等車</button>
  <div id="feedbackBar">
    <button id="fbBoarded">✅ 有搭上</button>
    <button id="fbMissed">❌ 沒搭上</button>
    <button id="fbNotThis">🚫 不是這台</button>
  </div>
  <audio id="player" style="display:none;"></audio>

  <script>
    const video = document.getElementById('camera');
    const canvas = document.getElementById('canvas');
    const statusEl = document.getElementById('status');
    const hintEl = document.getElementById('hint');
    const player = document.getElementById('player');
    const tapLayer = document.getElementById('tapLayer');
    const wizard = document.getElementById('wizard');
    const notifyBtn = document.getElementById('notifyBtn');
    const resultScreen = document.getElementById('resultScreen');
    const resultText = document.getElementById('resultText');
    const feedbackBar = document.getElementById('feedbackBar');

    let busy = false;
    let lastRoute = null;
    let lastEventId = null;
    let profile = { impairment_type: '', visible_radius_percent: 100, font_size_px: 32, theme: 'dark', voice_enabled: true };
    let chosenType = '';

    // ---- 行程資訊（要搭的路線＋站牌）：接 TDX 即時到站資料 ----
    let tripRoute = localStorage.getItem('trip_route') || '';
    let tripStop = localStorage.getItem('trip_stop') || '';
    const tripBar = document.getElementById('tripBar');

    async function fetchTdxEta() {
      if (!tripRoute || !tripStop) return;
      try {
        const res = await fetch('/api/tdx_eta?route=' + encodeURIComponent(tripRoute) + '&stop=' + encodeURIComponent(tripStop));
        const data = await res.json();
        if (data.status === 'success' && data.candidates && data.candidates.length > 0 && data.candidates[0].eta_minutes !== null) {
          tripBar.innerText = tripRoute + ' 號公車預計 ' + data.candidates[0].eta_minutes + ' 分鐘後到站（' + tripStop + '）';
        } else {
          tripBar.innerText = tripRoute + ' 號公車 @ ' + tripStop + '（暫無即時資料）';
        }
      } catch (err) {
        tripBar.innerText = tripRoute + ' 號公車 @ ' + tripStop;
      }
    }

    function updateTripBar() {
      if (tripRoute && tripStop) {
        tripBar.innerText = tripRoute + ' 號公車 @ ' + tripStop;
        fetchTdxEta();
      } else {
        tripBar.innerText = '點擊設定要搭的公車路線與站牌';
      }
    }

    tripBar.addEventListener('click', (e) => {
      e.stopPropagation();
      const r = prompt('請輸入公車路線號碼（例如 307）', tripRoute);
      if (r === null) return;
      const s = prompt('請輸入站牌名稱（例如 公館）', tripStop);
      if (s === null) return;
      tripRoute = r.trim();
      tripStop = s.trim();
      localStorage.setItem('trip_route', tripRoute);
      localStorage.setItem('trip_stop', tripStop);
      updateTripBar();
    });

    setInterval(fetchTdxEta, 20000);

    function getUserId() {
      let id = localStorage.getItem('user_id');
      if (!id) {
        id = 'u_' + Math.random().toString(36).slice(2, 10);
        localStorage.setItem('user_id', id);
      }
      return id;
    }
    const userId = getUserId();

    function showWizStep(id) {
      document.querySelectorAll('.wizStep').forEach(s => s.classList.remove('active'));
      document.getElementById(id).classList.add('active');
    }

    async function checkProfile() {
      const res = await fetch('/api/profile?user_id=' + userId);
      const data = await res.json();
      if (data.exists) {
        profile = Object.assign(profile, data.profile);
        wizard.style.display = 'none';
        initCamera();
      } else {
        wizard.style.display = 'flex';
        showWizStep('step1');
      }
    }

    // 設定按鈕：重新打開精靈，並帶入目前已儲存的設定值
    document.getElementById('settingsBtn').addEventListener('click', async (e) => {
      e.stopPropagation();
      chosenType = profile.impairment_type || '隧道視野';
      fovSlider.value = profile.visible_radius_percent || 60;
      fovSlider.dispatchEvent(new Event('input'));
      fontSlider.value = profile.font_size_px || 32;
      fontSlider.dispatchEvent(new Event('input'));
      wizard.style.display = 'flex';
      showWizStep('step1');
      try {
        const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } });
        document.getElementById('fovVideo').srcObject = stream;
      } catch (err) { /* 相機預覽非必要，忽略 */ }
    });

    // Step 1：障礙類型
    document.querySelectorAll('#step1 .calBtn').forEach(btn => {
      btn.addEventListener('click', async () => {
        chosenType = btn.dataset.type;
        showWizStep('step2');
        try {
          const stream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } });
          document.getElementById('fovVideo').srcObject = stream;
        } catch (err) {
          document.getElementById('fovLabel').innerText = '無法開啟相機預覽，可直接用滑桿設定';
        }
      });
    });

    // Step 2：可視範圍滑桿
    const fovSlider = document.getElementById('fovSlider');
    fovSlider.addEventListener('input', () => {
      document.getElementById('fovMask').style.setProperty('--r', fovSlider.value + '%');
      document.getElementById('fovLabel').innerText = '目前設定：可視範圍 ' + fovSlider.value + '%';
    });
    document.getElementById('step2Next').addEventListener('click', () => showWizStep('step3'));

    // Step 3：字體大小
    const fontSlider = document.getElementById('fontSlider');
    fontSlider.addEventListener('input', () => {
      document.getElementById('fontPreview').style.fontSize = fontSlider.value + 'px';
    });
    document.getElementById('step3Next').addEventListener('click', () => showWizStep('step4'));

    // Step 4：色調 -> 存檔完成
    document.querySelectorAll('#step4 .calBtn').forEach(btn => {
      btn.addEventListener('click', async () => {
        profile = {
          impairment_type: chosenType,
          visible_radius_percent: parseInt(fovSlider.value, 10),
          font_size_px: parseInt(fontSlider.value, 10),
          theme: btn.dataset.theme,
          voice_enabled: true,
        };
        await fetch('/api/profile', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(Object.assign({ user_id: userId }, profile)),
        });
        wizard.style.display = 'none';
        initCamera();
      });
    });

    function applyTheme(el) {
      if (profile.theme === 'light') {
        el.style.background = '#fff';
        el.style.color = '#000';
      } else {
        el.style.background = '#000';
        el.style.color = '#fff';
      }
    }

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

    // 依可視範圍計算：每段顯示幾個字、停留幾秒（範圍越小，字越少、停越久）
    function computePacing(text) {
      const r = profile.visible_radius_percent || 100;
      const charsPerChunk = Math.max(8, Math.round(0.6 * r + 8));
      const secondsPerChunk = Math.max(2, Math.round(8 - r / 15));
      const chunks = [];
      for (let i = 0; i < text.length; i += charsPerChunk) {
        chunks.push(text.slice(i, i + charsPerChunk));
      }
      return { chunks, secondsPerChunk };
    }

    function showResult(text) {
      applyTheme(resultScreen);
      resultText.style.fontSize = (profile.font_size_px || 32) + 'px';
      resultScreen.style.display = 'flex';

      const { chunks, secondsPerChunk } = computePacing(text);
      let idx = 0;
      resultText.innerText = chunks[0] || text;
      if (chunks.length > 1) {
        const timer = setInterval(() => {
          idx++;
          if (idx >= chunks.length) { clearInterval(timer); return; }
          resultText.innerText = chunks[idx];
        }, secondsPerChunk * 1000);
      }
    }

    async function captureAndAnalyze() {
      if (busy) return;
      if (resultScreen.style.display === 'flex') {
        resultScreen.style.display = 'none';
        notifyBtn.style.display = 'none';
        feedbackBar.style.display = 'none';
        return;
      }
      busy = true;
      if (navigator.vibrate) navigator.vibrate(80);
      hintEl.style.display = 'none';
      statusEl.innerText = '分析中...';

      // 縮小到最長邊 1024px 再上傳，加快上傳與 Gemini 分析速度（辨識文字不需要原始解析度）
      const MAX_DIM = 1024;
      const scale = Math.min(1, MAX_DIM / Math.max(video.videoWidth, video.videoHeight));
      canvas.width = Math.round(video.videoWidth * scale);
      canvas.height = Math.round(video.videoHeight * scale);
      canvas.getContext('2d').drawImage(video, 0, 0, canvas.width, canvas.height);

      canvas.toBlob(async (blob) => {
        try {
          const formData = new FormData();
          formData.append('image', blob, 'scan.jpg');
          formData.append('user_id', userId);
          if (tripRoute) formData.append('route', tripRoute);
          if (tripStop) formData.append('stop_name', tripStop);
          const res = await fetch('/api/analyze', { method: 'POST', body: formData });
          const data = await res.json();

          if (data.status !== 'success') {
            statusEl.innerText = '失敗: ' + data.message;
          } else {
            statusEl.innerText = '點螢幕回到掃描';
            showResult(data.summary);
            if (profile.voice_enabled !== false) {
              player.src = data.audio_url;
              player.play();
            }
            lastRoute = data.route;
            lastEventId = data.event_id;
            feedbackBar.style.display = 'flex';
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

    async function sendFeedback(e, feedback, label) {
      e.stopPropagation();
      if (!lastEventId) return;
      await fetch('/api/feedback', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ event_id: lastEventId, feedback: feedback }),
      });
      statusEl.innerText = '已記錄：' + label;
      if (navigator.vibrate) navigator.vibrate(60);
    }
    document.getElementById('fbBoarded').addEventListener('click', (e) => sendFeedback(e, 'boarded', '有搭上'));
    document.getElementById('fbMissed').addEventListener('click', (e) => sendFeedback(e, 'missed', '沒搭上'));
    document.getElementById('fbNotThis').addEventListener('click', (e) => sendFeedback(e, 'not_this_one', '不是這台'));

    tapLayer.addEventListener('click', captureAndAnalyze);
    resultScreen.addEventListener('click', captureAndAnalyze);
    checkProfile();
    updateTripBar();
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
        visible_radius_percent=int(data.get("visible_radius_percent", 100)),
        font_size_px=int(data.get("font_size_px", 32)),
        theme=data.get("theme", "dark"),
        voice_enabled=data.get("voice_enabled", True),
    )
    return jsonify({"status": "success"})


@app.route("/api/tdx_eta", methods=["GET"])
def api_tdx_eta():
    route = request.args.get("route")
    stop = request.args.get("stop", "")
    city = request.args.get("city", "Taipei")
    if not route:
        return jsonify({"status": "error", "message": "缺少 route"}), 400
    try:
        candidates = tdx_client.get_eta_candidates(city, route, stop)
    except Exception as e:
        return jsonify({"status": "error", "message": str(e)}), 502
    return jsonify({"status": "success", "candidates": candidates})


@app.route("/api/analyze", methods=["POST"])
def api_analyze():
    file = request.files.get("image")
    if not file:
        return jsonify({"status": "error", "message": "缺少照片"}), 400

    user_id = request.form.get("user_id")
    trip_route = request.form.get("route")
    trip_stop = request.form.get("stop_name")

    job_id = str(uuid.uuid4())[:8]
    image_path = os.path.join(UPLOAD_DIR, f"{job_id}.jpg")
    file.save(image_path)

    audio_path = os.path.join(AUDIO_DIR, f"{job_id}.mp3")

    # 模組 3：用 TDX 即時到站資料縮小 AI Vision 的候選範圍（失敗不影響主流程）
    tdx_hint = None
    tdx_eta_minutes = None
    if trip_route and trip_stop:
        try:
            candidates = tdx_client.get_eta_candidates("Taipei", trip_route, trip_stop)
            tdx_hint = tdx_client.build_vision_hint(candidates, trip_route)
            if candidates and candidates[0]["eta_minutes"] is not None:
                tdx_eta_minutes = candidates[0]["eta_minutes"]
        except Exception:
            pass  # TDX 掛掉就當作沒有這個提示，繼續走純視覺辨識

    start_time = time.time()
    try:
        result = run_pipeline(image_path, audio_path, tdx_hint=tdx_hint)
        success = True
    except Exception as e:
        success = False
        db.log_recognition_event(
            user_id=user_id, stop_name=trip_stop, route=trip_route,
            success=False, duration_seconds=time.time() - start_time,
            impairment_type=None, used_tdx_hint=bool(tdx_hint),
        )
        return jsonify({"status": "error", "message": str(e)}), 500

    route_match = re.search(r"\d{2,4}", result["summary"])
    route = route_match.group() if route_match else None

    profile = db.get_user_profile(user_id) if user_id else None
    event_id = db.log_recognition_event(
        user_id=user_id, stop_name=trip_stop, route=route or trip_route,
        success=True, duration_seconds=time.time() - start_time,
        impairment_type=profile.get("impairment_type") if profile else None,
        used_tdx_hint=bool(tdx_hint),
    )

    return jsonify({
        "status": "success",
        "summary": result["summary"],
        "audio_url": f"/audio/{job_id}.mp3",
        "route": route,
        "tdx_eta_minutes": tdx_eta_minutes,
        "event_id": event_id,
    })


@app.route("/api/feedback", methods=["POST"])
def api_feedback():
    data = request.get_json(force=True)
    event_id = data.get("event_id")
    feedback = data.get("feedback")  # boarded / missed / not_this_one
    if not event_id or not feedback:
        return jsonify({"status": "error", "message": "缺少 event_id 或 feedback"}), 400
    db.update_event_feedback(event_id, feedback, data.get("note", ""))
    return jsonify({"status": "success"})


@app.route("/api/export.csv")
def api_export_csv():
    import csv
    import io

    events = db.list_recognition_events()
    output = io.StringIO()
    fieldnames = [
        "event_id", "created_at", "user_id", "stop_name", "route", "success",
        "duration_seconds", "impairment_type", "used_tdx_hint", "feedback", "feedback_note",
    ]
    writer = csv.DictWriter(output, fieldnames=fieldnames, extrasaction="ignore")
    writer.writeheader()
    for e in events:
        writer.writerow(e)

    return app.response_class(
        output.getvalue(),
        mimetype="text/csv",
        headers={"Content-Disposition": "attachment; filename=recognition_events.csv"},
    )


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
