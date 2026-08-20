const path = require("node:path");
const { test, expect } = require("@playwright/test");

const testImage = path.join(__dirname, "..", "test-assets", "bus-stop-test.png");



test("calibration stores the clicked point as viewport percentage", async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 667 });
  await page.goto("http://localhost:8080/passenger");
  await page.waitForSelector("#screen-calib-1.active");

  await page.locator('.choice-card[data-impairment="tunnel_vision"]').click();
  await page.locator("#btn-step1-next").click();
  await page.waitForSelector("#screen-calib-2.active");

  const box = await page.locator("#calib-canvas").boundingBox();
  const clickX = Math.round(box.x + box.width * 0.3);
  const clickY = Math.round(box.y + box.height * 0.25);
  await page.mouse.click(clickX, clickY);

  const viewport = await page.evaluate(() => ({ w: window.innerWidth, h: window.innerHeight }));
  const zone = await page.evaluate(() => state.safeZone);
  expect(zone.x).toBeCloseTo((clickX / viewport.w) * 100, 1);
  expect(zone.y).toBeCloseTo((clickY / viewport.h) * 100, 1);

  
  const marker = await page.evaluate(() => ({
    left: parseFloat(document.querySelector("#safe-zone-marker").style.left),
    top: parseFloat(document.querySelector("#safe-zone-marker").style.top),
    visible: document.querySelector("#safe-zone-marker").classList.contains("visible"),
  }));
  expect(marker.visible).toBe(true);
  expect(marker.left).toBeCloseTo(clickX - box.x, 1);
  expect(marker.top).toBeCloseTo(clickY - box.y, 1);
});




test("safe zone display verify", async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 667 });
  await page.goto("http://localhost:8080/passenger");
  await page.evaluate(async () => {
    const uid = "zone_user_" + Date.now();
    localStorage.setItem("user_id", uid);
    await fetch("/api/profile", {
      method: "POST",
      headers: { "Content-Type": "application/json", "X-User-Id": uid },
      body: JSON.stringify({
        user_id: uid,
        impairment_type: "tunnel_vision",
        safe_zone: { x: 72, y: 28, radius: 95 },
        font_scale: 1.5,
        voice_enabled: false,
      }),
    });
  });
  await page.reload();
  await page.waitForResponse((res) => res.url().endsWith("/api/profile"));
  await page.evaluate(() => showScreen("screen-camera"));
  await page.locator("#manual-upload-btn").click();
  await page.locator("#manual-photo-input").setInputFiles(testImage);
  await page.waitForSelector("#screen-result.active", { timeout: 20000 });

  const style = await page.evaluate(() => {
    const el = document.querySelector("#safe-zone-window");
    const cs = getComputedStyle(el);
    return {
      position: cs.position,
      left: cs.left,
      top: cs.top,
      width: cs.width,
      height: cs.height,
      transform: cs.transform,
      zoneScale: cs.getPropertyValue("--zone-scale").trim(),
    };
  });
  console.log("zone window style:", JSON.stringify(style, null, 2));

  
  expect(style.position).toBe("fixed");
  
  expect(style.width).toBe("190px");
  expect(style.height).toBe("190px");
  expect(parseFloat(style.zoneScale)).toBeCloseTo(95 / 60, 5);
  
  expect(style.left).toBe("270px");
  
  expect(parseFloat(style.top)).toBeCloseTo(186.76, 0);
  expect(style.transform).toContain("matrix"); 
});
