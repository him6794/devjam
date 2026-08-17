const path = require("node:path");
const { test, expect } = require("@playwright/test");

const testImage = path.join(__dirname, "..", "test-assets", "bus-stop-test.png");
const visualEvidenceDir = path.join(__dirname, "visual");

for (const width of [375, 768, 1280]) {
  test(`manual upload states render at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 });
    await page.addInitScript(() => {
      Object.defineProperty(navigator, "geolocation", {
        configurable: true,
        value: {
          getCurrentPosition(_success, error) {
            error({ code: 1 });
          },
        },
      });
    });

    const profileResponse = page.waitForResponse((response) => response.url().endsWith("/api/profile"));
    await page.goto("http://localhost:8080/passenger");
    await profileResponse;
    await page.addStyleTag({ content: "html { scrollbar-width: none; } ::-webkit-scrollbar { width: 0; height: 0; }" });
    await page.evaluate(() => showScreen("screen-camera"));
    await page.locator("#manual-test-location").focus();
    await page.screenshot({
      path: path.join(visualEvidenceDir, `${width}-camera-focus.png`),
      fullPage: true,
    });

    let releaseAnalyze;
    const analyzePaused = new Promise((resolve) => {
      releaseAnalyze = resolve;
    });
    await page.route("**/api/analyze", async (route) => {
      await analyzePaused;
      await route.continue();
    });

    await page.locator("#manual-photo-input").setInputFiles(testImage);
    await expect(page.locator("#manual-upload-status")).toContainText("使用新益里測試 GPS");
    await page.screenshot({
      path: path.join(visualEvidenceDir, `${width}-uploading.png`),
      fullPage: true,
    });

    releaseAnalyze();
    await expect(page.locator("#screen-result")).toHaveClass(/active/, { timeout: 15000 });
    await page.screenshot({
      path: path.join(visualEvidenceDir, `${width}-result.png`),
      fullPage: true,
    });
    await page.unroute("**/api/analyze");

    const secondProfileResponse = page.waitForResponse((response) => response.url().endsWith("/api/profile"));
    await page.reload();
    await secondProfileResponse;
    await page.addStyleTag({ content: "html { scrollbar-width: none; } ::-webkit-scrollbar { width: 0; height: 0; }" });
    await page.evaluate(() => showScreen("screen-camera"));
    await page.locator("#manual-test-location").uncheck();
    await page.locator("#manual-photo-input").setInputFiles(testImage);
    await expect(page.locator("#manual-upload-status")).toHaveText("無法取得定位，請允許位置權限後再試。");
    await page.screenshot({
      path: path.join(visualEvidenceDir, `${width}-gps-error.png`),
      fullPage: true,
    });
  });
}
