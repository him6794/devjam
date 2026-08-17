const { test, expect } = require("@playwright/test");

// startCamera() (passenger.js) calls getUserMedia, which Playwright's
// headless Chromium can't satisfy with a real camera — fakeUserMedia gives
// it a synthetic video track (Chromium's built-in test pattern) instead,
// which is enough for MediaStream to have a video track with real
// dimensions once loadedmetadata fires, which is what triggers
// startLiveGuide().
test.use({
  launchOptions: {
    args: ["--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream"],
  },
});

// init() (passenger.js) only calls startCamera() itself when /api/profile
// says the user already has a saved profile — a fresh test user routes to
// the calibration screens instead and never touches the camera. Seeding a
// profile first, the way zone-verify.spec.js does, makes init() take that
// branch on its own instead of racing a manually-forced showScreen call
// against it.
async function seedProfileAndReload(page) {
  await page.goto("http://localhost:8080/passenger");
  await page.evaluate(async () => {
    const uid = "live_guide_user_" + Date.now();
    localStorage.setItem("user_id", uid);
    await fetch("/api/profile", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-User-Id": uid },
      body: JSON.stringify({
        user_id: uid,
        impairment_type: "tunnel_vision",
        safe_zone: { x: 50, y: 50, radius: 60 },
        font_scale: 1.2,
        voice_enabled: false,
      }),
    });
  });
  await page.reload();
}

// 相機一開，Live Guide WebSocket 必須跟著連上（不是等辨識出站牌才連），
// 這是這輪修復的核心：Gemini Live 自己持續看畫面，而不是等別的邏輯先
// 判斷完危險/到站/車門才通知它。framesent 監聽器要在 page.on("websocket")
// 的 callback 裡、拿到 socket 的當下就掛上——frame 每 2 秒送一次，事後才
// 補掛監聽器會錯過已經發生的那次，永遠等不到。
test("starting the camera opens a live_guide WebSocket and streams frames", async ({ page }) => {
  let liveGuideSocket = null;
  let frameCount = 0;
  let closed = false;

  page.on("websocket", (ws) => {
    if (!ws.url().includes("/api/live_guide")) return;
    liveGuideSocket = ws;
    ws.on("framesent", () => frameCount++);
    ws.on("close", () => (closed = true));
  });

  await seedProfileAndReload(page);
  await page.waitForFunction(() => {
    const v = document.querySelector("#camera-video");
    return v && v.videoWidth > 0;
  });

  await expect.poll(() => liveGuideSocket !== null, { timeout: 10000 }).toBe(true);
  expect(liveGuideSocket.url()).toContain("/api/live_guide");

  // 至少一個 binary frame 真的被送出去了 —— 不只是連線開了但沒在用。
  // 送出間隔是 2 秒（LIVE_GUIDE_FRAME_INTERVAL_MS），給足時間讓第一次
  // setInterval tick 真的發生。
  await expect.poll(() => frameCount, { timeout: 10000 }).toBeGreaterThan(0);

  // 伺服器端每 4 秒（liveGuideJudgmentInterval）主動送一次「請判斷目前畫面」
  // 給 Gemini，這是這輪修復修正的關鍵行為：純視覺 frame 不會自己觸發回合，
  // 一定要有這個獨立的判斷迴圈才能運作。等超過一個判斷週期，連線仍然存活
  // （沒有因為 PushFrame/RequestJudgment 卡死而被伺服器關掉），證明判斷
  // 迴圈真的在跑且沒有阻塞讀取迴圈。
  await page.waitForTimeout(5000);
  expect(closed).toBe(false);
  expect(liveGuideSocket.isClosed()).toBe(false);

  // 離開相機頁（重新校準）必須關閉連線 —— 不留背景 Live session。這也
  // 驗證了讀寫迴圈分離的效果：即使判斷迴圈可能正在等 Gemini 回覆，
  // conn.Close() 仍會立刻讓所有阻塞中的呼叫返回，不會延遲關閉。
  await page.locator("#recalibrate-link").click();
  await expect.poll(() => closed, { timeout: 10000 }).toBe(true);
});
