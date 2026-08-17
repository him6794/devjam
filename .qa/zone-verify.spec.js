const path = require("node:path");
const { test, expect } = require("@playwright/test");

const testImage = path.join(__dirname, "..", "test-assets", "bus-stop-test.png");

// 校準畫面點擊處必須存成 viewport 百分比（不是 canvas 百分比），
// 結果頁的 fixed 資訊窗才對得到同一點。
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

  // marker 在 canvas 內跟點擊同點（px 換算）
  const marker = await page.evaluate(() => ({
    left: parseFloat(document.querySelector("#safe-zone-marker").style.left),
    top: parseFloat(document.querySelector("#safe-zone-marker").style.top),
    visible: document.querySelector("#safe-zone-marker").classList.contains("visible"),
  }));
  expect(marker.visible).toBe(true);
  expect(marker.left).toBeCloseTo(clickX - box.x, 1);
  expect(marker.top).toBeCloseTo(clickY - box.y, 1);
});

// 驗證結果資訊窗顯示在校準設定的視野範圍內：視野區設在右上
// (x=72, y=28, radius=95)，資訊窗的 left/top/寬高與字級縮放
// (--zone-scale) 都必須對齊校準值——這是視障使用者「看得見資訊」的關鍵。
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

  // fixed + viewport 百分比：與校準畫面的點擊座標同一套基準
  expect(style.position).toBe("fixed");
  // radius=95 → 視窗 190px；scale = 95/60 = 1.583（clamp 內）
  expect(style.width).toBe("190px");
  expect(style.height).toBe("190px");
  expect(parseFloat(style.zoneScale)).toBeCloseTo(95 / 60, 5);
  // left = clamp(95px, 72%, 100% - 95px)：375px 畫面 72% = 270px，上限 280px → 270px
  expect(style.left).toBe("270px");
  // top = clamp(95px, 28%, 100% - 95px)：667px 畫面 28% ≈ 186.76px
  expect(parseFloat(style.top)).toBeCloseTo(186.76, 0);
  expect(style.transform).toContain("matrix"); // translate(-50%,-50%) 已套用
});
