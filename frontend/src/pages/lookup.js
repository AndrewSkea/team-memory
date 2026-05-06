import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { parseIndex } from "../services/indexmd.js";
import { incrementLookups } from "../services/stats.js";

export function renderLookup(root, { config, toast }) {
  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");

  root.innerHTML = `
    <div class="card">
      <h2>Lookup</h2>
      <label>Search</label>
      <input id="q" type="text" placeholder="keywords across INDEX and entry headers" />
      <div id="results" style="margin-top:12px;"></div>
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
      e.path.toLowerCase().includes(query) ||
      e.topics.toLowerCase().includes(query)
    );
    const entryHits = [];
    for (const e of fileHits.slice(0, 5)) {
      const cached = await cache.get("file:" + e.path) ?? await fetchAndCache(gh, cache, e.path);
      const lines = cached.content.split(/\r?\n/);
      lines.forEach((l, i) => {
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
  const fileList = fileHits.map(e => `<li><a href="${url(e.path)}" target="_blank">${e.path}</a> <span class="muted">${e.topics}</span></li>`).join("");
  const entryList = entryHits.map(h => `<li><a href="${url(h.path, h.line)}" target="_blank">${h.header}</a> <span class="muted">${h.path}</span></li>`).join("");
  return `
    <div class="result">
      <b>Files (${fileHits.length})</b>
      <ul>${fileList || "<li class='muted'>none</li>"}</ul>
      <b>Entries (${entryHits.length})</b>
      <ul>${entryList || "<li class='muted'>none</li>"}</ul>
    </div>
  `;
}
