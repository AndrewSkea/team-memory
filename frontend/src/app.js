import { api } from "./services/api.js";
import { escapeHtml } from "./services/html.js";
import { renderRemember } from "./pages/remember.js";
import { renderSearch } from "./pages/search.js";
import { renderReview } from "./pages/review.js";
import { renderMeetings } from "./pages/meetings.js";
import { renderStats } from "./pages/stats.js";
import { renderSettings } from "./pages/settings.js";

let root = document.getElementById("root");
const nav = document.getElementById("nav");
const footer = document.getElementById("footer");

export function toast(msg, isError = false) {
  const t = document.createElement("div");
  t.className = "toast" + (isError ? " error" : "");
  t.setAttribute("role", isError ? "alert" : "status");
  t.textContent = msg;
  document.body.appendChild(t);
  setTimeout(() => t.remove(), isError ? 6000 : 3500);
}

const state = { settings: null, page: "remember" };

const PAGES = {
  remember: renderRemember,
  search: renderSearch,
  review: renderReview,
  meetings: renderMeetings,
  stats: renderStats,
  settings: renderSettings,
};

async function refreshSettings() {
  try {
    state.settings = await api.settings();
  } catch (e) {
    state.settings = null;
    toast(e.message, true);
  }
  updateChrome();
  return state.settings;
}

function updateChrome() {
  const s = state.settings;
  const badge = document.getElementById("review-badge");
  if (badge) {
    const n = s?.pending_count ?? 0;
    badge.textContent = n > 0 ? String(n) : "";
    badge.hidden = n === 0;
  }
  if (s?.configured) {
    footer.innerHTML = `<span class="footer-dot connected"></span>${escapeHtml(s.username || s.owner)} · ${escapeHtml(s.owner)}/${escapeHtml(s.repo)} · mode: ${escapeHtml(s.effective_mode)}`;
  } else {
    footer.innerHTML = `<span class="footer-dot"></span>Not set up`;
  }
}

export async function go(page) {
  if (!state.settings?.configured && page !== "settings") page = "settings";
  if (!PAGES[page]) page = "remember";
  state.page = page;
  if (location.hash !== "#" + page) history.replaceState(null, "", "#" + page);
  for (const b of nav.querySelectorAll("button[data-page]")) {
    const active = b.dataset.page === page;
    b.classList.toggle("active", active);
    if (active) b.setAttribute("aria-current", "page"); else b.removeAttribute("aria-current");
  }
  nav.hidden = !state.settings?.configured;
  // A fresh container per render drops listeners the previous page attached.
  const fresh = root.cloneNode(false);
  root.replaceWith(fresh);
  root = fresh;
  await PAGES[page](root, { settings: state.settings, toast, go, refreshSettings });
}

// Back/forward and #page links route without a reload.
window.addEventListener("hashchange", () => {
  const page = location.hash.replace("#", "");
  if (page && page !== state.page) go(page);
});

nav.addEventListener("click", e => {
  const b = e.target.closest("button[data-page]");
  if (b) go(b.dataset.page);
});

async function bootstrap() {
  try {
    const h = await api.health();
    const sub = document.querySelector(".subtitle");
    if (sub && h.version) sub.textContent = `v${h.version.replace(/^v/, "")}`;
  } catch { /* shown by refreshSettings */ }
  await refreshSettings();
  const wanted = location.hash.replace("#", "") || "remember";
  await go(wanted);
  // Keep the review badge fresh (captures land in the background).
  setInterval(() => { if (document.visibilityState === "visible") refreshSettings(); }, 60_000);
}

bootstrap();
