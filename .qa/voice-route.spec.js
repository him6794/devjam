const path = require("node:path");
const { test, expect } = require("@playwright/test");

const testImage = path.join(__dirname, "..", "test-assets", "bus-stop-test.png");

// Same sequencing rule as manual-upload.spec.js: wait for /api/profile
// before forcing the camera screen, or init()'s own showScreen call races
// ours and hides the controls mid-test.
async function gotoCameraScreen(page) {
  const profileResponse = page.waitForResponse((res) => res.url().endsWith("/api/profile"));
  await page.goto("http://localhost:8080/passenger");
  await profileResponse;
  await page.evaluate(() => showScreen("screen-camera"));
}

// 新益里（manual-upload 的測試 GPS 點）有 307 路線，所以文字輸入 307 後
// 上傳測試照片，結果頁必須把 307 標成「你要搭的路線」並排第一。
test("text route input marks the wanted route in analyze results", async ({ page }) => {
  await gotoCameraScreen(page);
  await page.locator("#route-input").fill("我要搭307路");
  await page.locator("#btn-set-route").click();

  await expect(page.locator("#route-status")).toContainText("已設定路線：307");

  // analyze 回應會把 wanted_route 原樣回傳，用回應驗證請求確實帶上了
  const analyzeResponse = page.waitForResponse((res) => res.url().endsWith("/api/analyze"));
  await page.locator("#manual-upload-btn").click();
  await page.locator("#manual-photo-input").setInputFiles(testImage);

  await expect(page.locator("#screen-result")).toHaveClass(/active/, { timeout: 15000 });
  await expect(page.locator("#result-route")).toHaveText("307");
  await expect(page.locator("#result-wanted")).toHaveText("✓ 你要搭的路線");

  const analyze = await (await analyzeResponse).json();
  expect(analyze.wanted_route).toBe("307");
  expect(analyze.wanted_route_found).toBe(true);
  expect(analyze.buses[0].route).toBe("307");
  expect(analyze.buses[0].is_wanted).toBe(true);
});

// 聽不出路線時要明確告知，而不是靜默失敗。
test("text route input explains when no route number is heard", async ({ page }) => {
  await gotoCameraScreen(page);
  await page.locator("#route-input").fill("今天天氣真好");
  await page.locator("#btn-set-route").click();

  await expect(page.locator("#route-status")).toHaveText("沒有聽到路線號碼，請說出例如「307」。");
});
