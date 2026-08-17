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

document.addEventListener("DOMContentLoaded", init);

async function init() {
  bindOnboardingEvents();
  bindResultEvents();
  bindRouteSetup();
  qs("#recalibrate-link").addEventListener("click", () => {
    showScreen("screen-calib-1");
  });

  const userId = getUserId();
  const profileRes = USE_MOCK
    ? await MockAPI.getProfile(userId)
    : await apiFetch(`/api/profile`, { headers: { "X-User-Id": userId } });

  if (profileRes.exists) {
    state.impairmentType = profileRes.impairment_type;
    state.safeZone = profileRes.safe_zone;
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

  function setMarkerPosition(xPercent, yPercent) {
    marker.style.left = xPercent + "%";
    marker.style.top = yPercent + "%";
    marker.classList.add("visible");
    state.safeZone.x = xPercent;
    state.safeZone.y = yPercent;
    qs("#btn-step2-next").disabled = false;
  }

  canvas.addEventListener("click", (e) => {
    const rect = canvas.getBoundingClientRect();
    const xPercent = ((e.clientX - rect.left) / rect.width) * 100;
    const yPercent = ((e.clientY - rect.top) / rect.height) * 100;
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
  state.currentBuses = data.buses;
  state.currentVoiceSummary = data.voice_summary;
  state.currentVoiceAudioUrl = data.voice_audio_url || "";
  state.currentRoute = data.buses[0]?.route;
  state.currentStationName = data.station_name || state.currentStationName;

  applyFontScale(data.display.font_scale || state.fontScale);
  applySafeZonePosition(qs("#safe-zone-window"), data.display.safe_zone_position);

  const main = data.buses[0];
  qs("#result-route").textContent = main.route;
  qs("#result-eta").textContent = `${main.eta_minutes} 分鐘`;
  qs("#result-direction").textContent = main.direction;

  // 想要的路線：有就醒目標記；說了但這站沒有，更要大聲講（視障使用者
  // 不會自己掃清單找不存在的路線）
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

  const others = data.buses.slice(1);
  const listEl = qs("#other-buses-list");
  listEl.innerHTML = "";
  listEl.classList.remove("expanded");
  others.forEach((bus) => {
    const item = document.createElement("div");
    item.className = "other-bus-item";
    item.innerHTML = `
      <span class="route">${bus.route}</span>
      <span class="eta">${bus.eta_minutes} 分鐘・${bus.direction}</span>
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
