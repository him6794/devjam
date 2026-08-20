

const MockAPI = {
  
  async getProfile(userId) {
    await delay(300);
    const local = getProfileLocal();
    if (local) {
      return { exists: true, ...local };
    }
    return { exists: false };
  },

  
  async saveProfile(profile) {
    await delay(300);
    saveProfileLocal(profile);
    return { status: "success" };
  },

  
  
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
      station_name: "捷運公館站",
      buses: [
        { route: "307", eta_minutes: 3, direction: "往台北車站", urgency: "high", is_wanted: false },
        { route: "202", eta_minutes: 8, direction: "往公館", urgency: "medium", is_wanted: false },
        { route: "88", eta_minutes: 15, direction: "往新店", urgency: "low", is_wanted: false },
      ],
      wanted_route: "",
      wanted_route_found: false,
      display: { safe_zone_position: "top", font_scale: 1.5 },
      voice_summary: "307路線3分鐘後到站，往台北車站方向",
      voice_audio_url: "",
    };
  },

  
  async voiceRoute(payload) {
    await delay(400);
    const text = payload.text || "";
    const m = /\d+/.exec(text);
    if (m) {
      return { status: "success", route: m[0], transcript: text };
    }
    return { status: "no_route", message: "沒有聽到路線號碼，請說出例如「307」。" };
  },

  
  async notifyDriver(payload) {
    await delay(400);
    return { status: "success", alert_id: "alert_" + Date.now() };
  },

  
  
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
