const MANUAL_TEST_LOCATION = Object.freeze({
  name: "新益里",
  lat: 25.062218,
  lng: 121.566257,
});

let manualUploadInProgress = false;

document.addEventListener("DOMContentLoaded", bindManualUploadEvents);

function bindManualUploadEvents() {
  const button = qs("#manual-upload-btn");
  const input = qs("#manual-photo-input");

  button.addEventListener("click", () => {
    if (!manualUploadInProgress) input.click();
  });

  input.addEventListener("change", () => {
    const file = input.files[0];
    input.value = "";
    if (file) uploadManualPhoto(file);
  });

  qs("#btn-scan-again").addEventListener("click", clearManualUploadStatus);
  qs("#recalibrate-link").addEventListener("click", clearManualUploadStatus);
}

function setManualUploadStatus(message, tone = "") {
  const status = qs("#manual-upload-status");
  status.textContent = message;
  status.classList.toggle("success", tone === "success");
  status.classList.toggle("error", tone === "error");
}

function clearManualUploadStatus() {
  setManualUploadStatus();
}

async function uploadManualPhoto(file) {
  if (manualUploadInProgress) return;

  if (!file.type.startsWith("image/")) {
    setManualUploadStatus("請選擇 JPG、PNG 或其他圖片檔案。", "error");
    return;
  }

  const button = qs("#manual-upload-btn");
  const testLocationToggle = qs("#manual-test-location");
  const useTestLocation = testLocationToggle.checked;
  manualUploadInProgress = true;
  button.disabled = true;
  testLocationToggle.disabled = true;
  button.setAttribute("aria-busy", "true");
  window.scrollTo(0, 0);
  const locationLabel = useTestLocation
    ? `使用${MANUAL_TEST_LOCATION.name}測試 GPS`
    : "定位";
  setManualUploadStatus(`已選擇 ${file.name}，${locationLabel}分析中...`);

  try {
    let data;
    if (USE_MOCK) {
      data = await MockAPI.analyze(getUserId(), null, null, null);
    } else {
      const position = useTestLocation
        ? MANUAL_TEST_LOCATION
        : await getGpsSafe();
      if (!position) {
        setManualUploadStatus("無法取得定位，請允許位置權限後再試。", "error");
        return;
      }

      const formData = new FormData();
      formData.append("user_id", getUserId());
      formData.append("image", file, file.name);
      formData.append("lat", position.lat);
      formData.append("lng", position.lng);
      if (state.wantedRoute) formData.append("wanted_route", state.wantedRoute);
      data = await apiFetch("/api/analyze", { method: "POST", body: formData });
    }

    if (data.status === "success" && data.buses && data.buses.length > 0) {
      setManualUploadStatus("上傳完成，正在顯示辨識結果。", "success");
      renderResult(data);
      showScreen("screen-result");
      return;
    }

    if (data.status === "success") {
      setManualUploadStatus("已找到站牌，但目前查不到即時到站資訊，請稍後再試。", "error");
      return;
    }

    const message = data.status === "not_found"
      ? "此位置附近找不到公車站，請換用公車站 GPS。"
      : "找不到公車資訊，請換一張照片再試。";
    setManualUploadStatus(message, "error");
  } catch {
    setManualUploadStatus("上傳失敗，請換一張照片再試。", "error");
  } finally {
    manualUploadInProgress = false;
    button.disabled = false;
    testLocationToggle.disabled = false;
    button.removeAttribute("aria-busy");
  }
}
