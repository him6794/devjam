/* ============================================
   假資料 — 模擬後端四支 API
   USE_MOCK = true 時，passenger.js / driver.js 會呼叫這裡而不是真的打網路
   後端串好後，把 common.js 裡的 USE_MOCK 改成 false 即可，
   邏輯完全不用改，因為回傳格式已對齊真實 API 合約
   ============================================ */

const MockAPI = {
  // 模擬 GET /api/profile
  async getProfile(userId) {
    await delay(300);
    const local = getProfileLocal();
    if (local) {
      return { exists: true, ...local };
    }
    return { exists: false };
  },

  // 模擬 POST /api/profile
  async saveProfile(profile) {
    await delay(300);
    saveProfileLocal(profile);
    return { status: "success" };
  },

  // 模擬 POST /api/analyze
  // 前幾次呼叫故意回 not_found，模擬「持續掃描中」，第3次才成功
  _analyzeAttempts: 0,
  async analyze(userId, imageBlob, lat, lng) {
    await delay(700);
    this._analyzeAttempts++;
    if (this._analyzeAttempts < 3) {
      return { status: "not_found", message: "尚未偵測到站牌" };
    }
    this._analyzeAttempts = 0;
    return {
      status: "success",
      buses: [
        { route: "307", eta_minutes: 3, direction: "往台北車站", urgency: "high" },
        { route: "202", eta_minutes: 8, direction: "往公館", urgency: "medium" },
        { route: "88", eta_minutes: 15, direction: "往新店", urgency: "low" },
      ],
      display: { safe_zone_position: "top", font_scale: 1.5 },
      voice_summary: "307路線3分鐘後到站，往台北車站方向",
    };
  },

  // 模擬 POST /api/notify_driver
  async notifyDriver(payload) {
    await delay(400);
    return { status: "success", alert_id: "alert_" + Date.now() };
  },

  // 模擬 GET /api/driver_alerts?route=X
  // 呼叫第2次以後才「生出」一筆新警示，模擬輪詢過程中收到新資料
  _alertPollCount: 0,
  _mockAlerts: [],
  async getDriverAlerts(route) {
    await delay(300);
    this._alertPollCount++;
    if (this._alertPollCount === 2) {
      this._mockAlerts.push({
        alert_id: "alert_" + Date.now(),
        station_name: "捷運公館站",
        impairment_type: "tunnel_vision",
        timestamp: new Date().toISOString(),
        acknowledged: false,
      });
    }
    return { alerts: this._mockAlerts };
  },
};

function delay(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
