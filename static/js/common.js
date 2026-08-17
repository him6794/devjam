/* ============================================
   共用工具函式 — 乘客端/司機端都會用到
   ============================================ */

// 是否使用假資料開發（後端已串好，走同源 /api/*）
const USE_MOCK = false;
const API_BASE = ""; // 例如 "https://your-backend.a.run.app"，Flask同源部署可留空

/**
 * 取得裝置專屬使用者 ID，第一次進站自動產生並存起來
 */
function getUserId() {
  let id = localStorage.getItem("user_id");
  if (!id) {
    id = "u_" + Date.now() + "_" + Math.random().toString(36).slice(2, 8);
    localStorage.setItem("user_id", id);
  }
  return id;
}

function saveProfileLocal(profile) {
  localStorage.setItem("profile_cache", JSON.stringify(profile));
}

function getProfileLocal() {
  const raw = localStorage.getItem("profile_cache");
  return raw ? JSON.parse(raw) : null;
}

/**
 * 把 font_scale 套用到全域 CSS 變數，所有字級會連動放大
 */
function applyFontScale(scale) {
  document.documentElement.style.setProperty("--font-scale", scale);
}

/**
 * 統一的 fetch 包裝，之後接真實後端只要把 USE_MOCK 關掉
 */
async function apiFetch(path, options = {}) {
  const res = await fetch(API_BASE + path, options);
  if (!res.ok) {
    throw new Error(`API ${path} 回應錯誤: ${res.status}`);
  }
  return res.json();
}

function qs(selector, root = document) {
  return root.querySelector(selector);
}

function qsa(selector, root = document) {
  return Array.from(root.querySelectorAll(selector));
}

function showScreen(id) {
  qsa(".screen").forEach((s) => s.classList.remove("active"));
  qs("#" + id).classList.add("active");
  window.scrollTo(0, 0);
}

function getGpsSafe() {
  return new Promise((resolve) => {
    if (!navigator.geolocation) return resolve(null);
    navigator.geolocation.getCurrentPosition(
      (pos) => resolve({ lat: pos.coords.latitude, lng: pos.coords.longitude }),
      () => resolve(null),
      { timeout: 3000 }
    );
  });
}
