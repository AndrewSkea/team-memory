import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { parseIndex } from "../services/indexmd.js";

const STALE_DAYS = 90;

export function renderStale(root, { config, toast }) {
  root.innerHTML = `
    <div class="card">
      <div class="section-label">Stale entries (&gt;${STALE_DAYS} days old)</div>
      <div id="stale-body">
        <p style="color:var(--muted);font-size:14px;">Loading…</p>
      </div>
    </div>
  `;

  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");

  loadStale(gh, cache).then(items => {
    const body = document.getElementById("stale-body");
    if (!items.length) {
      body.innerHTML = `<div class="stale-empty">No stale entries found — memory bank is fresh!</div>`;
      return;
    }
    body.innerHTML = items.map(item => `
      <div class="result-item">
        <div style="display:flex;justify-content:space-between;align-items:flex-start;gap:8px;">
          <div>
            <div style="font-weight:600;font-size:14px;">${item.title}</div>
            <div style="font-size:12px;color:var(--muted);margin-top:2px;">${item.file} · ${item.age} days ago</div>
          </div>
          <a href="https://github.com/${config.owner}/${config.repo}/blob/master/${item.file}" target="_blank"
             style="font-size:12px;color:var(--accent);white-space:nowrap;">View →</a>
        </div>
      </div>
    `).join("");
  }).catch(e => {
    document.getElementById("stale-body").innerHTML =
      `<p style="color:var(--danger);font-size:14px;">Failed to load: ${e.message}</p>`;
  });
}

async function loadStale(gh, cache) {
  let idxFile = await cache.get("file:INDEX.md");
  if (!idxFile) {
    idxFile = await gh.getFile("INDEX.md");
    await cache.set("file:INDEX.md", idxFile);
  }
  const idx = parseIndex(idxFile.content);
  const now = Date.now();
  const cutoff = STALE_DAYS * 24 * 60 * 60 * 1000;
  const stale = [];

  for (const entry of idx.entries.slice(0, 10)) {
    const cached = await cache.get("file:" + entry.path) ?? await fetchAndCache(gh, cache, entry.path);
    const lines = cached.content.split(/\r?\n/);
    for (const line of lines) {
      if (line.startsWith("### Entry:")) {
        const tsMatch = line.match(/(\d{4}-\d{2}-\d{2}T[\d:.Z+-]+)/);
        if (tsMatch) {
          const age = Math.floor((now - new Date(tsMatch[1]).getTime()) / (24 * 60 * 60 * 1000));
          if (age > STALE_DAYS) {
            const title = line.replace(/^### Entry:\s*/, "").replace(/\s*\|.*$/, "").trim();
            stale.push({ title, file: entry.path, age });
          }
        }
      }
    }
  }

  return stale.sort((a, b) => b.age - a.age).slice(0, 20);
}

async function fetchAndCache(gh, cache, path) {
  const f = await gh.getFile(path);
  await cache.set("file:" + path, f);
  return f;
}
