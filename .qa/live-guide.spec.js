const { test, expect } = require("@playwright/test");







test.use({
  launchOptions: {
    args: ["--use-fake-device-for-media-stream", "--use-fake-ui-for-media-stream"],
  },
});







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

  
  
  
  await expect.poll(() => frameCount, { timeout: 10000 }).toBeGreaterThan(0);

  
  
  
  
  
  await page.waitForTimeout(5000);
  expect(closed).toBe(false);
  expect(liveGuideSocket.isClosed()).toBe(false);

  
  
  
  await page.locator("#recalibrate-link").click();
  await expect.poll(() => closed, { timeout: 10000 }).toBe(true);
});
