


const USE_MOCK = false;
const API_BASE = ""; 


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


function applyFontScale(scale) {
  document.documentElement.style.setProperty("--font-scale", scale);
}


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
