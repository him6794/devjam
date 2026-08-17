const state = {
  impairmentType: null,
  safeZone: { x: 50, y: 50, radius: 30 }, // 百分比座標
  fontScale: 1,
  voiceEnabled: true,
  currentBuses: [],
  currentVoiceSummary: "",
  currentRoute: null,
  currentStationName: "捷運公館站",
};

let scanning = false;
let scanTimer = null;
let scanAttempts = 0;
const MAX_SCAN_ATTEMPTS = 6;
const SCAN_INTERVAL_MS = 2000;
let mediaStream = null;

document.addEventListener("DOMContentLoaded", init);

async function init() {
  bindOnboardingEvents();
  bindResultEvents();
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
      const blob = await new Promise((resolve) => canvas.toBlob(resolve, "image/jpeg", 0.85));
      const formData = new FormData();
      formData.append("user_id", getUserId());
      formData.append("image", blob, "frame.jpg");
      const pos = await getGpsSafe();
      if (pos) {
        formData.append("lat", pos.lat);
        formData.append("lng", pos.lng);
      }
      data = await apiFetch(`/api/analyze`, { method: "POST", body: formData });
    }
  } catch (err) {
    stopScanning();
    qs("#camera-status").textContent = "辨識發生錯誤，請再試一次";
    qs("#camera-status").classList.add("error");
    return;
  }

  if (data.status === "success") {
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
  state.currentRoute = data.buses[0]?.route;

  applyFontScale(data.display.font_scale || state.fontScale);
  applySafeZonePosition(qs("#safe-zone-window"), data.display.safe_zone_position);

  const main = data.buses[0];
  qs("#result-route").textContent = main.route;
  qs("#result-eta").textContent = `${main.eta_minutes} 分鐘`;
  qs("#result-direction").textContent = main.direction;

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

function playVoiceSummary() {
  if (!state.currentVoiceSummary || !("speechSynthesis" in window)) return;
  window.speechSynthesis.cancel();
  const utter = new SpeechSynthesisUtterance(state.currentVoiceSummary);
  utter.lang = "zh-TW";
  window.speechSynthesis.speak(utter);
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
