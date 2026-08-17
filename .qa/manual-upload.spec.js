const path = require("node:path");
const { test, expect } = require("@playwright/test");

const testImage = path.join(__dirname, "..", "test-assets", "bus-stop-test.png");

// init() in passenger.js asynchronously fetches /api/profile on load and,
// for a fresh test user (no saved profile), calls showScreen("screen-calib-1")
// once that resolves. Forcing showScreen("screen-camera") before that
// response lands races init()'s own call — whichever fires last wins, which
// intermittently flips the screen back and hides the manual-upload controls
// mid-test. Waiting for the response first makes the sequencing deterministic.
async function gotoCameraScreen(page) {
  const profileResponse = page.waitForResponse((res) => res.url().endsWith("/api/profile"));
  await page.goto("http://localhost:8080/passenger");
  await profileResponse;
  await page.evaluate(() => showScreen("screen-camera"));
}

test("manual upload uses the known bus-stop GPS when current location has no stop", async ({ page }) => {
  await page.addInitScript(() => {
    window.__geoCalls = 0;
    Object.defineProperty(navigator, "geolocation", {
      configurable: true,
      value: {
        getCurrentPosition(_success, error) {
          window.__geoCalls += 1;
          error({ code: 1 });
        },
      },
    });
  });

  let analyzeRequest;
  page.on("request", (request) => {
    if (request.url().endsWith("/api/analyze")) analyzeRequest = request;
  });

  await gotoCameraScreen(page);
  await page.locator("#manual-upload-btn").click();
  await page.locator("#manual-photo-input").setInputFiles(testImage);

  await expect(page.locator("#screen-result")).toHaveClass(/active/, { timeout: 15000 });
  await expect(page.locator("#result-route")).not.toHaveText("");
  expect(analyzeRequest).toBeTruthy();
  expect(await page.evaluate(() => window.__geoCalls)).toBe(0);
});

test("manual upload keeps the GPS error path when test location is disabled", async ({ page }) => {
  await page.addInitScript(() => {
    window.__geoCalls = 0;
    Object.defineProperty(navigator, "geolocation", {
      configurable: true,
      value: {
        getCurrentPosition(_success, error) {
          window.__geoCalls += 1;
          error({ code: 1 });
        },
      },
    });
  });

  let analyzeRequested = false;
  page.on("request", (request) => {
    if (request.url().endsWith("/api/analyze")) analyzeRequested = true;
  });

  await gotoCameraScreen(page);
  await page.locator("#manual-test-location").uncheck();
  await page.locator("#manual-photo-input").setInputFiles(testImage);

  await expect(page.locator("#manual-upload-status")).toHaveText("無法取得定位，請允許位置權限後再試。");
  expect(await page.locator("#screen-result").getAttribute("class")).not.toContain("active");
  expect(await page.evaluate(() => window.__geoCalls)).toBe(1);
  expect(analyzeRequested).toBe(false);
});

test("manual upload explains when live GPS is outside the bus-stop radius", async ({ page }) => {
  await page.addInitScript(() => {
    window.__geoCalls = 0;
    Object.defineProperty(navigator, "geolocation", {
      configurable: true,
      value: {
        getCurrentPosition(success) {
          window.__geoCalls += 1;
          success({ coords: { latitude: -40, longitude: 160 } });
        },
      },
    });
  });

  let analyzeRequested = false;
  page.on("request", (request) => {
    if (request.url().endsWith("/api/analyze")) analyzeRequested = true;
  });

  await gotoCameraScreen(page);
  await page.locator("#manual-test-location").uncheck();
  await page.locator("#manual-photo-input").setInputFiles(testImage);

  await expect(page.locator("#manual-upload-status")).toHaveText("此位置附近找不到公車站，請換用公車站 GPS。");
  expect(await page.locator("#screen-result").getAttribute("class")).not.toContain("active");
  expect(await page.evaluate(() => window.__geoCalls)).toBe(1);
  expect(analyzeRequested).toBe(true);
});
