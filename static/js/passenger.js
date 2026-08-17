const state = {
  impairmentType: null,
  safeZone: { x: 50, y: 50, radius: 30 }, // 百分比座標
  fontScale: 1,
  voiceEnabled: true,
  currentBuses: [],
  currentVoiceSummary: "",
  currentVoiceAudioUrl: "",
  currentRoute: null,
  currentStationName: "",
  wantedRoute: "", // 使用者說出/輸入想搭的路線（api.md §5），空字串表示未設定
};

let scanning = false;
let scanTimer = null;
let scanAttempts = 0;
const MAX_SCAN_ATTEMPTS = 6;
const SCAN_INTERVAL_MS = 2000;
let mediaStream = null;
let voiceAudio = null;
let routeRecorder = null;
let routeRecorderTimer = null;
const MAX_ROUTE_RECORD_SECONDS = 4;

// ===== Live Guide（Gemini Live 即時視覺協助）=====
// 相機一開就跟著連線、持續送畫面；模型自己看畫面判斷有沒有危險／公車進站／
// 車門位置需要提醒，多數畫面不會有回覆——這不是「等別的邏輯先判斷完才通知
// Live API」，是 Live API 自己在看。連線跟相機同壽命：相機關閉/離開頁面時
// 一起關掉，不留著背景連線耗費資源或觸發使用者沒預期的語音。
let liveGuideSocket = null;
let liveGuideTimer = null;
let liveGuideGpsTimer = null;
let liveGuideReconnectTimer = null;
let liveGuideReconnectDelay = 1000; // exponential backoff: 1s → 2s → 4s → 8s (cap)
// 每秒一幀：對環境變化（公車進站、車門開啟）的反應比 2 秒快一倍
const LIVE_GUIDE_FRAME_INTERVAL_MS = 1000;
const LIVE_GUIDE_GPS_INTERVAL_MS = 10000;
const LIVE_GUIDE_MAX_RECONNECT_DELAY = 8000;

// ===== Live Guide 狀態追蹤（給面板用）=====
const lgStats = {
  state: "idle",       // idle | connecting | connected | reconnecting | disconnected
  frames: 0,
  replies: 0,
  reconnects: 0,
  lastReply: "—",
};

function updateLiveGuidePanel() {
  const dot = document.getElementById("live-guide-dot");
  const elStatus = document.getElementById("lg-status");
  const elFrames = document.getElementById("lg-frames");
  const elReplies = document.getElementById("lg-replies");
  const elLast = document.getElementById("lg-last-reply");
  const elReconnects = document.getElementById("lg-reconnects");
  if (!dot) return; // 面板 DOM 還沒載入

  dot.setAttribute("data-state", lgStats.state);

  const stateLabels = {
    idle: "未啟動",
    connecting: "🟡 連線中…",
    connected: "🟢 已連線",
    reconnecting: "🟠 重連中…",
    disconnected: "🔴 已斷線",
  };
  if (elStatus) elStatus.textContent = stateLabels[lgStats.state] || lgStats.state;
  if (elFrames) elFrames.textContent = lgStats.frames;
  if (elReplies) elReplies.textContent = lgStats.replies;
  if (elLast) elLast.textContent = lgStats.lastReply;
  if (elReconnects) elReconnects.textContent = lgStats.reconnects;
}

// 面板展開/收合
document.addEventListener("DOMContentLoaded", () => {
  const toggle = document.getElementById("live-guide-toggle");
  const panel = document.getElementById("live-guide-panel");
  if (toggle && panel) {
    toggle.addEventListener("click", () => {
      panel.classList.toggle("collapsed");
    });
  }
  updateLiveGuidePanel();
});

function startLiveGuide() {
  if (liveGuideSocket) return; // 已經連著了
  clearTimeout(liveGuideReconnectTimer);
  liveGuideReconnectTimer = null;

  lgStats.state = "connecting";
  updateLiveGuidePanel();

  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  liveGuideSocket = new WebSocket(`${proto}//${location.host}/api/live_guide`);
  liveGuideSocket.binaryType = "arraybuffer";

  liveGuideSocket.addEventListener("open", () => {
    liveGuideReconnectDelay = 1000; // 連上後重設 backoff
    lgStats.state = "connected";
    updateLiveGuidePanel();
    liveGuideTimer = setInterval(sendLiveGuideFrame, LIVE_GUIDE_FRAME_INTERVAL_MS);
    startLiveGuideMic();
    sendLiveGuideGps();
    liveGuideGpsTimer = setInterval(sendLiveGuideGps, LIVE_GUIDE_GPS_INTERVAL_MS);
  });

  liveGuideSocket.addEventListener("message", (event) => {
    // 伺服器只在模型判斷「這個畫面需要提醒」時才會送訊息——多數畫面完全
    // 不會收到任何 message，這正是模型自己判斷、而非被動等通知的結果。
    // 模型回覆是純文字，由裝置自己的 TTS 唸出（視障使用者已設好的語速與語音）。
    let text = typeof event.data === "string" ? event.data : "";
    if (text) {
      if (text.includes("[FIND_STATION]")) {
        text = text.replace(/\[FIND_STATION\]/g, "").trim();
        // 模型現在應改用 station_status 工具查詢；保留這個觸發當後備
        if (!scanning) startScanning();
      }
      if (text) {
        lgStats.replies++;
        lgStats.lastReply = text.length > 30 ? text.slice(0, 30) + "…" : text;
        updateLiveGuidePanel();
        speakLiveGuideAlert(text);
      }
    }
  });

  liveGuideSocket.addEventListener("close", () => stopLiveGuide(true));
  liveGuideSocket.addEventListener("error", () => stopLiveGuide(true));
}

// ===== Live Guide GPS 推送 =====
// station_status 工具需要位置：前端定期把 GPS 送給後端存著，模型呼叫工具時
// 後端自動代入，模型永遠不需要知道經緯度數字。
async function sendLiveGuideGps() {
  if (!liveGuideSocket || liveGuideSocket.readyState !== WebSocket.OPEN) return;
  const pos = await getGpsSafe();
  if (!pos) return;
  const json = new TextEncoder().encode(JSON.stringify({ lat: pos.lat, lng: pos.lng }));
  const payload = new Uint8Array(1 + json.length);
  payload[0] = 0x02; // GPS marker
  payload.set(json, 1);
  liveGuideSocket.send(payload.buffer);
}

// shouldReconnect: true 表示非使用者主動關閉（連線斷掉、伺服器錯誤），
// 只要相機還開著就應該自動重連；false 表示使用者主動離開（重新校準、
// 離開頁面），不應重連。
function stopLiveGuide(shouldReconnect) {
  clearInterval(liveGuideTimer);
  liveGuideTimer = null;
  clearInterval(liveGuideGpsTimer);
  liveGuideGpsTimer = null;
  if (liveGuideSocket) {
    const socket = liveGuideSocket;
    liveGuideSocket = null; // 先清空，避免 close 事件的 stopLiveGuide 重入
    socket.close();
  }
  if (shouldReconnect && mediaStream) {
    // 相機還開著，代表使用者還在用——自動重連，exponential backoff 避免
    // 伺服器持續拒絕時打滿連線。
    lgStats.state = "reconnecting";
    lgStats.reconnects++;
    updateLiveGuidePanel();
    clearTimeout(liveGuideReconnectTimer);
    liveGuideReconnectTimer = setTimeout(() => {
      liveGuideReconnectTimer = null;
      if (mediaStream && !liveGuideSocket) startLiveGuide();
    }, liveGuideReconnectDelay);
    liveGuideReconnectDelay = Math.min(liveGuideReconnectDelay * 2, LIVE_GUIDE_MAX_RECONNECT_DELAY);
  } else {
    lgStats.state = shouldReconnect ? "disconnected" : "idle";
    updateLiveGuidePanel();
    stopLiveGuideMic();
  }
}

function cancelLiveGuideReconnect() {
  clearTimeout(liveGuideReconnectTimer);
  liveGuideReconnectTimer = null;
  liveGuideReconnectDelay = 1000;
}

window.addEventListener("pagehide", () => { cancelLiveGuideReconnect(); stopLiveGuide(false); });

function sendLiveGuideFrame() {
  if (!liveGuideSocket || liveGuideSocket.readyState !== WebSocket.OPEN) return;
  const video = qs("#camera-video");
  if (!video || !video.videoWidth) return;

  const canvas = document.createElement("canvas");
  canvas.width = video.videoWidth;
  canvas.height = video.videoHeight;
  canvas.getContext("2d").drawImage(video, 0, 0, canvas.width, canvas.height);
  canvas.toBlob(
    (blob) => {
      if (blob && liveGuideSocket && liveGuideSocket.readyState === WebSocket.OPEN) {
        blob.arrayBuffer().then((buf) => {
          const payload = new Uint8Array(1 + buf.byteLength);
          payload[0] = 0x00; // Video marker
          payload.set(new Uint8Array(buf), 1);
          liveGuideSocket.send(payload.buffer);
          lgStats.frames++;
          updateLiveGuidePanel();
        });
      }
    },
    "image/jpeg",
    0.7
  );
}

// ===== Live Guide 語音輸入 =====
let liveGuideAudioContext = null;
let liveGuideAudioProcessor = null;
let liveGuideMicStream = null;

async function startLiveGuideMic() {
  if (liveGuideMicStream) return;
  try {
    liveGuideMicStream = await navigator.mediaDevices.getUserMedia({ audio: true });
    liveGuideAudioContext = new (window.AudioContext || window.webkitAudioContext)({ sampleRate: 16000 });
    const source = liveGuideAudioContext.createMediaStreamSource(liveGuideMicStream);
    liveGuideAudioProcessor = liveGuideAudioContext.createScriptProcessor(4096, 1, 1);
    liveGuideAudioProcessor.onaudioprocess = (e) => {
      if (!liveGuideSocket || liveGuideSocket.readyState !== WebSocket.OPEN) return;
      const pcmFloat = e.inputBuffer.getChannelData(0);
      const pcm16 = new Int16Array(pcmFloat.length);
      for (let i = 0; i < pcmFloat.length; i++) {
        let s = Math.max(-1, Math.min(1, pcmFloat[i]));
        pcm16[i] = s < 0 ? s * 0x8000 : s * 0x7FFF;
      }
      const payload = new Uint8Array(1 + pcm16.byteLength);
      payload[0] = 0x01; // Audio marker
      payload.set(new Uint8Array(pcm16.buffer), 1);
      liveGuideSocket.send(payload.buffer);
    };
    source.connect(liveGuideAudioProcessor);
    liveGuideAudioProcessor.connect(liveGuideAudioContext.destination);
  } catch (err) {
    console.error("Live Guide Mic Error:", err);
  }
}

function stopLiveGuideMic() {
  if (liveGuideAudioProcessor) {
    liveGuideAudioProcessor.disconnect();
    liveGuideAudioProcessor = null;
  }
  if (liveGuideAudioContext) {
    liveGuideAudioContext.close();
    liveGuideAudioContext = null;
  }
  if (liveGuideMicStream) {
    liveGuideMicStream.getTracks().forEach(t => t.stop());
    liveGuideMicStream = null;
  }
}

// 危險提醒優先權最高：直接打斷正在播放的路線複誦，因為「右前方有落差」
// 比任何一句公車資訊都更急迫。
function speakLiveGuideAlert(text) {
  if (!("speechSynthesis" in window)) return;
  window.speechSynthesis.cancel();
  const utter = new SpeechSynthesisUtterance(text);
  utter.lang = "zh-TW";
  window.speechSynthesis.speak(utter);
}

document.addEventListener("DOMContentLoaded", init);

async function init() {
  bindOnboardingEvents();
  bindResultEvents();
  bindRouteSetup();
  qs("#recalibrate-link").addEventListener("click", () => {
    cancelLiveGuideReconnect();
    stopLiveGuide(false);
    showScreen("screen-calib-1");
  });

  const userId = getUserId();
  const profileRes = USE_MOCK
    ? await MockAPI.getProfile(userId)
    : await apiFetch(`/api/profile`, { headers: { "X-User-Id": userId } });

  if (profileRes.exists) {
    state.impairmentType = profileRes.impairment_type;
    state.safeZone = profileRes.safe_zone || state.safeZone;
    state.fontScale = profileRes.font_scale;
    state.voiceEnabled = profileRes.voice_enabled;
    applyFontScale(state.fontScale);
    showScreen("screen-camera");
    startCamera();
  } else {
    showScreen("screen-calib-1");
  }
}


function bindOnboardingEvents() {
  qsa(".choice-card[data-impairment]").forEach((card) => {
    card.addEventListener("click", () => {
      qsa(".choice-card[data-impairment]").forEach((c) => c.classList.remove("selected"));
      card.classList.add("selected");
      state.impairmentType = card.dataset.impairment;
      qs("#btn-step1-next").disabled = false;
    });
  });

  qs("#btn-step1-next").addEventListener("click", () => {
    showScreen("screen-calib-2");
    setupCalibCanvas();
  });

  qs("#btn-step2-next").addEventListener("click", () => {
    showScreen("screen-calib-3");
  });

  const slider = qs("#font-scale-slider");
  slider.addEventListener("input", () => {
    const scale = parseFloat(slider.value);
    qs("#preview-text").style.setProperty("font-size", `calc(${scale}rem)`);
    state.fontScale = scale;
  });

  qs("#voice-toggle").addEventListener("change", (e) => {
    state.voiceEnabled = e.target.checked;
  });

  qs("#btn-step3-finish").addEventListener("click", finishCalibration);
}


function setupCalibCanvas() {
  const canvas = qs("#calib-canvas");
  const marker = qs("#safe-zone-marker");
  const sizeSlider = qs("#safe-zone-size-slider");

  function updateMarkerSize() {
    const size = state.safeZone.radius * 2;
    marker.style.width = size + "px";
    marker.style.height = size + "px";
  }

  // 存的是「視窗(viewport)百分比」而不是 canvas 百分比：結果頁的
  // 資訊窗用 position:fixed 直接對齊同一套座標，標記在哪、資訊就
  // 出現在哪（之前用 canvas 百分比，兩邊基準不同所以會跑掉）。
  function setMarkerPosition(xPercent, yPercent) {
    state.safeZone.x = xPercent;
    state.safeZone.y = yPercent;
    // marker 留在 canvas 內跟手指同點：viewport 座標換算回 canvas px
    const rect = canvas.getBoundingClientRect();
    marker.style.left = (xPercent / 100) * window.innerWidth - rect.left + "px";
    marker.style.top = (yPercent / 100) * window.innerHeight - rect.top + "px";
    marker.classList.add("visible");
    qs("#btn-step2-next").disabled = false;
  }

  canvas.addEventListener("click", (e) => {
    const xPercent = (e.clientX / window.innerWidth) * 100;
    const yPercent = (e.clientY / window.innerHeight) * 100;
    setMarkerPosition(xPercent, yPercent);
    updateMarkerSize();
  });

  sizeSlider.addEventListener("input", () => {
    state.safeZone.radius = parseInt(sizeSlider.value, 10);
    updateMarkerSize();
  });

  state.safeZone.radius = parseInt(sizeSlider.value, 10);
  updateMarkerSize();
}


async function finishCalibration() {
  const profile = {
    user_id: getUserId(),
    impairment_type: state.impairmentType,
    safe_zone: state.safeZone,
    font_scale: state.fontScale,
    voice_enabled: state.voiceEnabled,
  };

  if (USE_MOCK) {
    await MockAPI.saveProfile(profile);
  } else {
    await apiFetch(`/api/profile`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(profile),
    });
  }

  applyFontScale(state.fontScale);
  showScreen("screen-camera");
  startCamera();
}


async function startCamera() {
  if (mediaStream) return; // 已經開過了，不要重複要求權限
  const video = qs("#camera-video");
  try {
    mediaStream = await navigator.mediaDevices.getUserMedia({
      video: { facingMode: "environment" },
      audio: false,
    });
    video.srcObject = mediaStream;
    video.addEventListener("loadedmetadata", startLiveGuide, { once: true });
  } catch (err) {
    qs("#camera-status").textContent = "無法開啟相機，請確認已允許權限";
    qs("#camera-status").classList.add("error");
  }
}

function bindResultEvents() {
  qs("#shutter-btn").addEventListener("click", () => {
    if (scanning) return;
    startScanning();
  });

  qs("#other-buses-toggle").addEventListener("click", () => {
    const list = qs("#other-buses-list");
    const expanded = list.classList.toggle("expanded");
    qs("#other-buses-toggle").textContent = expanded
      ? "收起 ▴"
      : `還有${state.currentBuses.length - 1}班車 ▾`;
  });

  qs("#btn-voice-replay").addEventListener("click", playVoiceSummary);

  qs("#btn-notify-driver").addEventListener("click", notifyDriver);

  qs("#btn-scan-again").addEventListener("click", () => {
    showScreen("screen-camera");
    qs("#camera-status").textContent = "";
    qs("#camera-status").classList.remove("error");
    // 回到相機畫面時，如果 Live Guide 已斷線，重新連上
    startLiveGuide();
  });
}

// ===== 路線選擇（Journey Agent v1，api.md §5）=====
// 語音（MediaRecorder 錄音 → /api/voice_route）或文字輸入，
// 成功後存進 state.wantedRoute，之後每次 /api/analyze 都帶上。
function bindRouteSetup() {
  qs("#btn-record-route").addEventListener("click", toggleRouteRecording);

  qs("#btn-set-route").addEventListener("click", () => {
    const raw = qs("#route-input").value.trim();
    if (!raw) {
      setRouteStatus("請先輸入或說出路線號碼。", "error");
      return;
    }
    qs("#route-input").value = "";
    submitVoiceRoute({ text: raw });
  });
  qs("#route-input").addEventListener("keydown", (e) => {
    if (e.key === "Enter") qs("#btn-set-route").click();
  });
}

async function toggleRouteRecording() {
  const btn = qs("#btn-record-route");
  if (routeRecorder && routeRecorder.state === "recording") {
    // 第二次點擊＝結束錄音；auto-stop 逾時也會走同一條 onstop 路徑
    btn.textContent = "🎤 辨識中...";
    btn.disabled = true;
    routeRecorder.stop();
    return;
  }

  let stream;
  try {
    stream = await navigator.mediaDevices.getUserMedia({ audio: true });
  } catch {
    setRouteStatus("無法使用麥克風，請改用下方文字輸入路線號碼。", "error");
    return;
  }

  const chunks = [];
  routeRecorder = new MediaRecorder(stream);
  routeRecorder.ondataavailable = (e) => {
    if (e.data.size) chunks.push(e.data);
  };
  routeRecorder.onstop = async () => {
    stream.getTracks().forEach((t) => t.stop());
    clearTimeout(routeRecorderTimer);
    const mime = routeRecorder.mimeType || "audio/webm";
    const blob = new Blob(chunks, { type: mime });
    const audioBase64 = await blobToBase64(blob);
    submitVoiceRoute({ audio_base64: audioBase64, audio_mime: mime });
  };

  routeRecorder.start();
  btn.textContent = "🎤 錄音中，再按一次結束";
  btn.classList.add("recording");
  // 提示說完整句子：單唸數字（「307」）是語音辨識最難的場景，
  // 「我要搭307路」有上下文，準確率高很多
  setRouteStatus("請說完整句子，例如「我要搭307路」", "");
  routeRecorderTimer = setTimeout(() => {
    if (routeRecorder && routeRecorder.state === "recording") routeRecorder.stop();
  }, MAX_ROUTE_RECORD_SECONDS * 1000);
}

async function submitVoiceRoute(payload) {
  try {
    const res = USE_MOCK
      ? await MockAPI.voiceRoute(payload)
      : await apiFetch(`/api/voice_route`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(payload),
        });

    resetRecordButton();

    if (res.status === "success" && res.route) {
      setWantedRoute(res.route);
      return;
    }
    setRouteStatus(res.message || "聽不清楚，請再說一次，或改用文字輸入。", "error");
    speakRoutePrompt("沒有聽到路線號碼，請再說一次，或使用下方輸入框輸入");
  } catch {
    // 503 stt_unavailable / stt_failed 等：提示改走文字輸入
    resetRecordButton();
    setRouteStatus("語音辨識失敗，請改用下方文字輸入路線號碼。", "error");
    qs("#route-input").focus();
  }
}

function resetRecordButton() {
  const btn = qs("#btn-record-route");
  btn.textContent = "🎤 說出想搭的路線";
  btn.disabled = false;
  btn.classList.remove("recording");
}

function setWantedRoute(route) {
  state.wantedRoute = route;
  setRouteStatus(`已設定路線：${route}。拍照後會優先顯示這條路線。`, "success");
  speakRoutePrompt(`已設定路線${route}，拍照後會優先顯示這條路線`);
}

function setRouteStatus(message, tone) {
  const status = qs("#route-status");
  status.textContent = message;
  status.classList.toggle("success", tone === "success");
  status.classList.toggle("error", tone === "error");
}

function speakRoutePrompt(text) {
  if (!state.voiceEnabled || !("speechSynthesis" in window)) return;
  window.speechSynthesis.cancel();
  const utter = new SpeechSynthesisUtterance(text);
  utter.lang = "zh-TW";
  window.speechSynthesis.speak(utter);
}

function blobToBase64(blob) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = String(reader.result || "");
      resolve(result.split(",")[1] || "");
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(blob);
  });
}

async function startScanning() {
  scanning = true;
  scanAttempts = 0;
  qs("#shutter-btn").classList.add("scanning");
  qs("#camera-status").textContent = "辨識中...";
  qs("#camera-status").classList.remove("error");
  scanTimer = setInterval(captureAndSend, SCAN_INTERVAL_MS);
  captureAndSend(); 
}

function stopScanning() {
  scanning = false;
  clearInterval(scanTimer);
  qs("#shutter-btn").classList.remove("scanning");
}

async function captureAndSend() {
  if (!scanning) return;
  scanAttempts++;

  const video = qs("#camera-video");
  const canvas = document.createElement("canvas");
  canvas.width = video.videoWidth || 640;
  canvas.height = video.videoHeight || 480;
  const ctx = canvas.getContext("2d");
  if (video.videoWidth) {
    ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
  }

  let data;
  try {
    if (USE_MOCK) {
      data = await MockAPI.analyze(getUserId(), null, null, null);
    } else {
      const pos = await getGpsSafe();
      if (!pos) {
        stopScanning();
        qs("#camera-status").textContent = "無法取得定位，請允許位置權限後再試";
        qs("#camera-status").classList.add("error");
        return;
      }

      const blob = await new Promise((resolve) => canvas.toBlob(resolve, "image/jpeg", 0.85));
      const formData = new FormData();
      formData.append("user_id", getUserId());
      formData.append("image", blob, "frame.jpg");
      formData.append("lat", pos.lat);
      formData.append("lng", pos.lng);
      if (state.wantedRoute) formData.append("wanted_route", state.wantedRoute);
      data = await apiFetch(`/api/analyze`, { method: "POST", body: formData });
    }
  } catch (err) {
    stopScanning();
    qs("#camera-status").textContent = "辨識發生錯誤，請再試一次";
    qs("#camera-status").classList.add("error");
    return;
  }

  if (data.status === "success" && data.buses && data.buses.length > 0) {
    stopScanning();
    renderResult(data);
    showScreen("screen-result");
  } else if (scanAttempts >= MAX_SCAN_ATTEMPTS) {
    stopScanning();
    qs("#camera-status").textContent = "找不到站牌，請調整角度或靠近一點";
    qs("#camera-status").classList.add("error");
  }
}

function renderResult(data) {
  if (!data || !data.buses || data.buses.length === 0) {
    qs("#result-route").textContent = "無法取得路線資訊";
    qs("#result-eta").textContent = "";
    qs("#result-direction").textContent = "";
    const wantedTag = qs("#result-wanted");
    wantedTag.textContent = "";
    wantedTag.style.display = "none";
    state.currentRoute = "";
    state.currentStationName = "";
    state.currentBuses = [];
    state.currentVoiceSummary = "";
    state.currentVoiceAudioUrl = "";
    applyFontScale(state.fontScale);
    applySafeZoneWindow(qs("#safe-zone-window"), state.safeZone);
    return;
  }

  const main = data.buses[0];
  const route = main.route || "無路線資訊";
  qs("#result-route").textContent = route;
  const eta = main.eta_minutes || 0;
  qs("#result-eta").textContent = `${eta} 分鐘`;
  qs("#result-direction").textContent = main.direction || "";

  // 想要的路線：有就醒目標記；說了但這站沒有，更要大聲講
  const wantedTag = qs("#result-wanted");
  if (main.is_wanted) {
    wantedTag.textContent = "✓ 你要搭的路線";
    wantedTag.classList.remove("missing");
    wantedTag.style.display = "block";
  } else if (data.wanted_route) {
    wantedTag.textContent = `⚠ 此站沒有${data.wanted_route}路`;
    wantedTag.classList.add("missing");
    wantedTag.style.display = "block";
  } else {
    wantedTag.style.display = "none";
  }

  state.currentBuses = data.buses;
  state.currentVoiceSummary = data.voice_summary || "";
  state.currentVoiceAudioUrl = data.voice_audio_url || "";
  state.currentRoute = route;
  state.currentStationName = data.station_name || "";

  applyFontScale(data.display?.font_scale || state.fontScale);
  applySafeZoneWindow(qs("#safe-zone-window"), state.safeZone);

  const others = data.buses.slice(1);
  const listEl = qs("#other-buses-list");
  listEl.innerHTML = "";
  listEl.classList.remove("expanded");
  others.forEach((bus) => {
    const busRoute = bus.route || "無路線";
    const busEta = bus.eta_minutes || 0;
    const busDir = bus.direction || "";
    const item = document.createElement("div");
    item.className = "other-bus-item";
    item.innerHTML = `
      <span class="route">${busRoute}</span>
      <span class="eta">${busEta} 分鐘・${busDir}</span>
    `;
    listEl.appendChild(item);
  });

  const toggle = qs("#other-buses-toggle");
  if (others.length > 0) {
    toggle.style.display = "block";
    toggle.textContent = `還有${others.length}班車 ▾`;
  } else {
    toggle.style.display = "none";
  }

  const notifyBtn = qs("#btn-notify-driver");
  notifyBtn.textContent = "📢 通知司機";
  notifyBtn.classList.remove("done");
  notifyBtn.disabled = false;

  if (state.voiceEnabled) {
    playVoiceSummary();
  }
}

// 把結果資訊窗放到校準時標記的「看得最清楚的地方」：x/y 是 viewport
// 百分比（與 setupCalibCanvas 同源），radius 是像素半徑（與校準滑桿
// 同源）。資訊窗用 position:fixed，所以 % 直接對齊 viewport，標記在哪
// 就出現在哪；視窗大小跟著範圍走、字級依範圍縮放（--zone-scale）。
function applySafeZoneWindow(el, zone) {
  const radius = clampNum(zone?.radius ?? 30, 24, 140);
  const x = clampNum(zone?.x ?? 50, 0, 100);
  const y = clampNum(zone?.y ?? 50, 0, 100);
  const half = radius; // 視窗 = 2 × 半徑

  el.style.width = el.style.height = `${radius * 2}px`;
  // fixed 元素的 % 以 viewport 為基準；clamp() 讓視窗就算在邊緣也不會跑出螢幕
  el.style.left = `clamp(${half}px, ${x}%, calc(100% - ${half}px))`;
  el.style.top = `clamp(${half}px, ${y}%, calc(100% - ${half}px))`;
  el.style.setProperty("--zone-scale", clampNum(radius / 60, 0.35, 2.5));
}

function clampNum(v, min, max) {
  return Math.min(Math.max(v, min), max);
}

// Device TTS is the primary path so the rider hears bus updates at their
// own already-configured screen-reader rate/voice, per plan.md §9 — a
// fixed-rate server audio clip would override that. The Cloud TTS file is
// only a fallback for browsers without speechSynthesis support.
function playVoiceSummary() {
  if ("speechSynthesis" in window && state.currentVoiceSummary) {
    window.speechSynthesis.cancel();
    const utter = new SpeechSynthesisUtterance(state.currentVoiceSummary);
    utter.lang = "zh-TW";
    window.speechSynthesis.speak(utter);
    return;
  }
  if (state.currentVoiceAudioUrl) {
    if (voiceAudio) voiceAudio.pause();
    voiceAudio = new Audio(state.currentVoiceAudioUrl);
    voiceAudio.play().catch(() => {});
  }
}

async function notifyDriver() {
  const payload = {
    route: state.currentRoute,
    station_name: state.currentStationName,
    impairment_type: state.impairmentType,
  };

  const res = USE_MOCK
    ? await MockAPI.notifyDriver(payload)
    : await apiFetch(`/api/notify_driver`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(payload),
      });

  if (res.status === "success") {
    const btn = qs("#btn-notify-driver");
    btn.textContent = "已通知 ✓";
    btn.classList.add("done");
    btn.disabled = true;
  }
}
