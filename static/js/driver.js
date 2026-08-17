/* ============================================
   司機端主邏輯
   使用情境跟乘客端不同：一般視力、正在準備發車，需要快速一瞥
   ============================================ */

const driverState = {
  route: null,
  knownAlertIds: new Set(),   // 已經渲染過的警示，避免重複插入
  acknowledgedIds: new Set(), // 已確認的警示 id
};

let pollTimer = null;
const POLL_INTERVAL_MS = 3000;

document.addEventListener("DOMContentLoaded", () => {
  qs("#route-select-bar").addEventListener("submit", (e) => {
    e.preventDefault();
    const route = qs("#route-input").value.trim();
    if (route) selectRoute(route);
  });
});

function selectRoute(route) {
  driverState.route = route;
  driverState.knownAlertIds.clear();
  driverState.acknowledgedIds.clear();
  qs("#monitoring-label").textContent = `目前監控：${route} 路線`;

  qs("#alert-list").innerHTML = "";
  qs("#alert-list").hidden = true;
  qs("#ack-list").innerHTML = "";
  qs("#ack-section").hidden = true;
  qs("#alert-empty").hidden = false;

  if (pollTimer) clearInterval(pollTimer);
  pollAlerts();
  pollTimer = setInterval(pollAlerts, POLL_INTERVAL_MS);
}

async function pollAlerts() {
  if (!driverState.route) return;

  const data = USE_MOCK
    ? await MockAPI.getDriverAlerts(driverState.route)
    : await apiFetch(`/api/driver_alerts?route=${encodeURIComponent(driverState.route)}`);

  const alerts = data.alerts || [];
  if (alerts.length === 0) return;

  qs("#alert-empty").hidden = true;
  qs("#alert-list").hidden = false;

  alerts.forEach((alert) => {
    if (driverState.knownAlertIds.has(alert.alert_id)) return;
    driverState.knownAlertIds.add(alert.alert_id);
    renderAlertCard(alert);
    playAlertSound();
  });
}

function renderAlertCard(alert) {
  const card = document.createElement("div");
  card.className = "alert-card";
  card.id = "alert-" + alert.alert_id;

  card.innerHTML = `
    <div class="bar"></div>
    <div class="body">
      <p class="station">${alert.station_name}</p>
      <p class="impairment">👁 ${impairmentLabel(alert.impairment_type)}乘客</p>
      <p class="time">${formatTimeAgo(alert.timestamp)}</p>
      <button class="ack-btn">已確認</button>
    </div>
  `;

  card.querySelector(".ack-btn").addEventListener("click", () => acknowledgeAlert(alert));

  qs("#alert-list").prepend(card);
}

function acknowledgeAlert(alert) {
  driverState.acknowledgedIds.add(alert.alert_id);

  const card = qs("#alert-" + alert.alert_id);
  card.classList.add("acknowledged");
  card.querySelector(".ack-btn").remove();

  qs("#ack-section").hidden = false;
  qs("#ack-list").appendChild(card);

  if (qs("#alert-list").children.length === 0) {
    qs("#alert-list").hidden = true;
    qs("#alert-empty").hidden = false;
  }
}

function impairmentLabel(type) {
  const map = {
    tunnel_vision: "隧道視野",
    central_scotoma: "中心視野缺損",
    other: "視障",
  };
  return map[type] || "視障";
}

function formatTimeAgo(isoString) {
  const diffMs = Date.now() - new Date(isoString).getTime();
  const mins = Math.max(0, Math.round(diffMs / 60000));
  if (mins < 1) return "剛剛";
  return `${mins} 分鐘前`;
}

function playAlertSound() {
  // 用短促的 Web Audio 蜂鳴取代外部音檔，避免多帶一個素材檔案
  try {
    const ctx = new (window.AudioContext || window.webkitAudioContext)();
    const osc = ctx.createOscillator();
    const gain = ctx.createGain();
    osc.frequency.value = 880;
    gain.gain.setValueAtTime(0.15, ctx.currentTime);
    gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + 0.3);
    osc.connect(gain).connect(ctx.destination);
    osc.start();
    osc.stop(ctx.currentTime + 0.3);
  } catch (err) {
    // 靜默失敗即可，音效只是加分不是必要功能
  }
}
