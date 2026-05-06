import { renderSetup } from "./pages/setup.js";
import { renderRemember } from "./pages/remember.js";
import { renderLookup } from "./pages/lookup.js";
import { renderStats } from "./pages/stats.js";

const CONFIG_KEY = "team-memory:config";

function loadConfig() {
  try { return JSON.parse(localStorage.getItem(CONFIG_KEY)) || {}; } catch { return {}; }
}
function saveConfig(c) { localStorage.setItem(CONFIG_KEY, JSON.stringify(c)); }

function toast(msg, isError = false) {
  const t = document.createElement("div");
  t.className = "toast" + (isError ? " error" : "");
  t.textContent = msg;
  document.body.appendChild(t);
  setTimeout(() => t.remove(), 3500);
}

const root = document.getElementById("root");
const nav = document.getElementById("nav");

function go(page) {
  const config = loadConfig();
  if (page !== "setup" && (!config.token || !config.owner)) {
    page = "setup";
  }
  for (const b of nav.querySelectorAll("button")) {
    b.classList.toggle("active", b.dataset.page === page);
  }
  if (page === "setup") {
    renderSetup(root, {
      config,
      onDone: c => { saveConfig(c); toast("Setup saved."); go("remember"); },
    });
  } else if (page === "remember") {
    renderRemember(root, { config, toast });
  } else if (page === "lookup") {
    renderLookup(root, { config, toast });
  } else if (page === "stats") {
    renderStats(root, { config, toast });
  } else {
    renderLookup(root, { config, toast });
  }
}

nav.addEventListener("click", e => {
  const b = e.target.closest("button[data-page]");
  if (b) go(b.dataset.page);
});

document.getElementById("forget-auth").addEventListener("click", () => {
  if (confirm("Clear saved credentials? You will need to re-enter them.")) {
    localStorage.removeItem(CONFIG_KEY);
    go("remember");
  }
});

go("remember");
