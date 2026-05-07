import { renderSetup } from "./pages/setup.js";
import { renderRemember } from "./pages/remember.js";
import { renderLookup } from "./pages/lookup.js";
import { renderStats } from "./pages/stats.js";
import { renderStale } from "./pages/stale.js";

const CONFIG_KEY = "team-memory:config";

function loadConfig() {
  try { return JSON.parse(localStorage.getItem(CONFIG_KEY)) || {}; } catch { return {}; }
}
function saveConfig(c) { localStorage.setItem(CONFIG_KEY, JSON.stringify(c)); }

export function toast(msg, isError = false) {
  const t = document.createElement("div");
  t.className = "toast" + (isError ? " error" : "");
  t.textContent = msg;
  document.body.appendChild(t);
  setTimeout(() => t.remove(), 3500);
}

const root = document.getElementById("root");
const nav = document.getElementById("nav");
const footer = document.getElementById("footer");

function updateFooter(config) {
  if (config.owner && config.repo) {
    footer.textContent = `${config.owner} · ${config.owner}/${config.repo}`;
  } else {
    footer.innerHTML = `<button id="forget-auth-footer" style="background:none;border:none;color:var(--muted);font-size:12px;cursor:pointer;">Forget auth</button>`;
    document.getElementById("forget-auth-footer")?.addEventListener("click", forgetAuth);
  }
}

function forgetAuth() {
  if (confirm("Clear saved credentials? You will need to re-enter them.")) {
    localStorage.removeItem(CONFIG_KEY);
    go("remember");
  }
}

export function go(page) {
  const config = loadConfig();
  if (page !== "setup" && (!config.token || !config.owner)) {
    page = "setup";
  }
  for (const b of nav.querySelectorAll("button[data-page]")) {
    b.classList.toggle("active", b.dataset.page === page);
  }
  updateFooter(config);
  if (page === "setup") {
    renderSetup(root, {
      config,
      onDone: c => { saveConfig(c); toast("Setup saved."); go("remember"); },
    });
  } else if (page === "remember") {
    renderRemember(root, { config, toast, forgetAuth });
  } else if (page === "lookup") {
    renderLookup(root, { config, toast });
  } else if (page === "stats") {
    renderStats(root, { config, toast });
  } else if (page === "stale") {
    renderStale(root, { config, toast });
  } else {
    renderLookup(root, { config, toast });
  }
}

nav.addEventListener("click", e => {
  const b = e.target.closest("button[data-page]");
  if (b) go(b.dataset.page);
});

go("remember");
