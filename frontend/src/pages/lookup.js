import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { parseIndex } from "../services/indexmd.js";
import { incrementLookups } from "../services/stats.js";

export function renderLookup(root, { config, toast }) {
  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");

  root.innerHTML = `
    <div class="card">
      <div class="section-label">Search memory bank</div>
      <div class="search-wrap">
        <svg viewBox="0 0 16 16"><circle cx="6.5" cy="6.5" r="4.5"/><line x1="10.5" y1="10.5" x2="14" y2="14"/></svg>
        <input id="q" class="search-input" type="text" placeholder="Keywords across files and entry headers…" autocomplete="off" />
      </div>
      <div id="results"></div>
    </div>
  `;

  const $ = s => root.querySelector(s);
  let timer;
  $("#q").oninput = () => {
    clearTimeout(timer);
    timer = setTimeout(() => doSearch($("#q").value, $("#results"), gh, cache, config, toast), 200);
  };
}

async function doSearch(query, out, gh, cache, config, toast) {
  query = query.trim().toLowerCase();
  if (!query) { out.innerHTML = ""; return; }
  incrementLookups();
  try {
    let idxFile = await cache.get("file:INDEX.md");
    if (!idxFile) {
      idxFile = await gh.getFile("INDEX.md");
      await cache.set("file:INDEX.md", idxFile);
    }
    const idx = parseIndex(idxFile.content);
    const fileHits = idx.entries.filter(e =>
      e.path.toLowerCase().includes(query) || e.topics.toLowerCase().includes(query)
    );
    const entryHits = [];
    for (const e of fileHits.slice(0, 5)) {
      const cached = await cache.get("file:" + e.path) ?? await fetchAndCache(gh, cache, e.path);
      cached.content.split(/\r?\n/).forEach((l, i) => {
        if (l.startsWith("### Entry:") && l.toLowerCase().includes(query)) {
          entryHits.push({ path: e.path, header: l, line: i });
        }
      });
    }
    out.innerHTML = renderResults(config, fileHits, entryHits);
  } catch (e) {
    toast("Lookup failed: " + e.message, true);
  }
}

async function fetchAndCache(gh, cache, path) {
  const f = await gh.getFile(path);
  await cache.set("file:" + path, f);
  return f;
}

function renderResults(config, fileHits, entryHits) {
  const url = (p, line) => `https://github.com/${config.owner}/${config.repo}/blob/master/${p}${line ? `#L${line + 1}` : ""}`;

  const fileItems = fileHits.map(e =>
    `<li><a href="${url(e.path)}" target="_blank">${e.path}</a> <span style="color:var(--muted);font-size:12px;">${e.topics}</span></li>`
  ).join("") || `<li style="color:var(--muted);">none</li>`;

  const entryItems = entryHits.map(h =>
    `<li><a href="${url(h.path, h.line)}" target="_blank">${h.header.replace("### Entry: ", "")}</a> <span style="color:var(--muted);font-size:12px;">${h.path}</span></li>`
  ).join("") || `<li style="color:var(--muted);">none</li>`;

  return `
    <div class="result-item">
      <div class="result-label">Files (${fileHits.length})</div>
      <ul class="result-list">${fileItems}</ul>
    </div>
    <div class="result-item" style="margin-top:8px;">
      <div class="result-label">Entries (${entryHits.length})</div>
      <ul class="result-list">${entryItems}</ul>
    </div>
  `;
}
