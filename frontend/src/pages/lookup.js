import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { parseIndex } from "../services/indexmd.js";
import { incrementLookups } from "../services/stats.js";
import { escapeHtml } from "../services/html.js";

export function extractTagsFromIndex(indexContent) {
  const matches = [...indexContent.matchAll(/\*\*Tags:\*\*\s*([^\n]+)/g)];
  const all = matches.flatMap(m => m[1].trim().split(';').map(t => t.trim()).filter(Boolean));
  return [...new Set(all)].sort();
}

export function filterByTag(entries, tag) {
  if (!tag) return entries;
  return entries.filter(e => e.tags && e.tags.includes(tag));
}

export function renderLookup(root, { config, toast }) {
  const gh = new GitHubClient({ token: config.token, owner: config.owner, repo: config.repo });
  const cache = new Cache("team-memory");

  root.innerHTML = `
    <div class="card">
      <div class="section-label">Search memory bank</div>
      <div id="tag-chips" style="display:flex;flex-wrap:wrap;gap:6px;margin-bottom:12px"></div>
      <div class="search-wrap">
        <svg viewBox="0 0 16 16"><circle cx="6.5" cy="6.5" r="4.5"/><line x1="10.5" y1="10.5" x2="14" y2="14"/></svg>
        <input id="q" class="search-input" type="text" placeholder="Keywords across files and entry headers…" autocomplete="off" />
      </div>
      <div id="results"></div>
    </div>
  `;

  const $ = s => root.querySelector(s);
  let timer;
  let activeTag = null;

  // Load index for tag chips
  (async () => {
    try {
      let idxFile = await cache.get("file:INDEX.md");
      if (!idxFile) {
        idxFile = await gh.getFile("INDEX.md");
        await cache.set("file:INDEX.md", idxFile);
      }
      if (idxFile && idxFile.exists) {
        const tags = extractTagsFromIndex(idxFile.content);
        const chipBar = $("#tag-chips");
        if (chipBar && tags.length > 0) {
          chipBar.innerHTML = tags.map(t =>
            `<button class="tag-chip" data-tag="${escapeHtml(t)}">${escapeHtml(t)}</button>`
          ).join('');
          chipBar.querySelectorAll('.tag-chip').forEach(btn => {
            btn.onclick = () => {
              activeTag = activeTag === btn.dataset.tag ? null : btn.dataset.tag;
              chipBar.querySelectorAll('.tag-chip').forEach(b =>
                b.classList.toggle('active', b.dataset.tag === activeTag));
              // Re-run search with current query + active tag filter
              doSearch($("#q").value, $("#results"), gh, cache, config, toast, activeTag);
            };
          });
        }
      }
    } catch (e) {
      console.error("lookup: failed to load tag index", e);
      toast("Lookup unavailable: " + (e?.message ?? "could not load index"), true);
    }
  })();

  $("#q").oninput = () => {
    clearTimeout(timer);
    timer = setTimeout(() => doSearch($("#q").value, $("#results"), gh, cache, config, toast, activeTag), 200);
  };
}

async function doSearch(query, out, gh, cache, config, toast, activeTag = null) {
  query = query.trim().toLowerCase();
  if (!query && !activeTag) { out.innerHTML = ""; return; }
  incrementLookups();
  try {
    let idxFile = await cache.get("file:INDEX.md");
    if (!idxFile) {
      idxFile = await gh.getFile("INDEX.md");
      await cache.set("file:INDEX.md", idxFile);
    }
    const idx = parseIndex(idxFile.content);
    let fileHits = idx.entries.filter(e =>
      !query || e.path.toLowerCase().includes(query) || e.topics.toLowerCase().includes(query)
    );
    const entryHits = [];
    for (const e of fileHits.slice(0, 5)) {
      const cached = await cache.get("file:" + e.path) ?? await fetchAndCache(gh, cache, e.path);
      const lines = cached.content.split(/\r?\n/);
      // Parse entries with their tags for filtering
      let currentEntry = null;
      lines.forEach((l, i) => {
        if (l.startsWith("### Entry:")) {
          currentEntry = { path: e.path, header: l, line: i, tags: [] };
          if (!query || l.toLowerCase().includes(query)) {
            entryHits.push(currentEntry);
          }
        } else if (currentEntry && l.startsWith("**Tags:**")) {
          const tagsStr = l.replace("**Tags:**", "").trim();
          currentEntry.tags = tagsStr.split(";").map(t => t.trim()).filter(Boolean);
        }
      });
    }
    const filtered = filterByTag(entryHits, activeTag);
    out.innerHTML = renderResults(config, fileHits, filtered);
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
    `<li><a href="${escapeHtml(url(e.path))}" target="_blank" rel="noopener noreferrer">${escapeHtml(e.path)}</a> <span style="color:var(--muted);font-size:12px;">${escapeHtml(e.topics)}</span></li>`
  ).join("") || `<li style="color:var(--muted);">none</li>`;

  const entryItems = entryHits.map(h =>
    `<li><a href="${escapeHtml(url(h.path, h.line))}" target="_blank" rel="noopener noreferrer">${escapeHtml(h.header.replace("### Entry: ", ""))}</a> <span style="color:var(--muted);font-size:12px;">${escapeHtml(h.path)}</span></li>`
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
