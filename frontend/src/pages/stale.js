import { GitHubClient } from "../services/github.js";
import { Cache } from "../services/cache.js";
import { parseIndex } from "../services/indexmd.js";

const STALE_DAYS = 90;

export function removeEntryFromContent(content, entryTitle) {
  const parts = content.split(/(?=### Entry:)/);
  const filtered = parts.filter(p => {
    if (!p.startsWith('### Entry:')) return true;
    const titleMatch = p.match(/^### Entry:\s*(.+)/);
    return !titleMatch || titleMatch[1].trim() !== entryTitle;
  });
  return filtered.join('');
}

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

    body.innerHTML = `
      <div style="display:flex;align-items:center;gap:8px;margin-bottom:12px;padding-bottom:10px;border-bottom:1px solid var(--border)">
        <label style="display:flex;align-items:center;gap:6px;font-size:13px;cursor:pointer">
          <input type="checkbox" id="select-all" />
          Select all
        </label>
        <div style="margin-left:auto;display:flex;gap:8px">
          <button id="archive-btn" class="btn-outline" style="padding:6px 14px;font-size:13px;" disabled>Archive selected</button>
          <button id="delete-btn" class="btn-outline" style="padding:6px 14px;font-size:13px;color:var(--danger);border-color:var(--danger);" disabled>Delete selected</button>
        </div>
      </div>
      <div id="stale-list">
        ${items.map((item, idx) => `
          <div class="result-item" id="stale-item-${idx}" data-title="${encodeURIComponent(item.title)}" data-file="${encodeURIComponent(item.file)}" data-block="${encodeURIComponent(item.block || '')}">
            <div style="display:flex;justify-content:space-between;align-items:flex-start;gap:8px;">
              <label style="display:flex;align-items:flex-start;gap:8px;cursor:pointer;flex:1">
                <input type="checkbox" class="stale-check" data-idx="${idx}" style="margin-top:3px;flex-shrink:0" />
                <div>
                  <div style="font-weight:600;font-size:14px;">${item.title}</div>
                  <div style="font-size:12px;color:var(--muted);margin-top:2px;">${item.file} · ${item.age} days ago</div>
                </div>
              </label>
              <a href="https://github.com/${config.owner}/${config.repo}/blob/master/${item.file}" target="_blank"
                 style="font-size:12px;color:var(--accent);white-space:nowrap;">View →</a>
            </div>
          </div>
        `).join("")}
      </div>
    `;

    // Wire checkboxes
    const selectAll = document.getElementById("select-all");
    const archiveBtn = document.getElementById("archive-btn");
    const deleteBtn = document.getElementById("delete-btn");
    const checks = () => [...document.querySelectorAll(".stale-check")];

    function updateBulkButtons() {
      const anyChecked = checks().some(c => c.checked);
      archiveBtn.disabled = !anyChecked;
      deleteBtn.disabled = !anyChecked;
    }

    selectAll.onchange = () => {
      checks().forEach(c => { c.checked = selectAll.checked; });
      updateBulkButtons();
    };

    document.querySelectorAll(".stale-check").forEach(cb => {
      cb.onchange = () => {
        selectAll.checked = checks().every(c => c.checked);
        updateBulkButtons();
      };
    });

    function getSelectedItems() {
      return checks()
        .filter(c => c.checked)
        .map(c => {
          const row = document.getElementById(`stale-item-${c.dataset.idx}`);
          return {
            title: decodeURIComponent(row.dataset.title),
            file: decodeURIComponent(row.dataset.file),
            block: decodeURIComponent(row.dataset.block),
          };
        });
    }

    archiveBtn.onclick = async () => {
      const selected = getSelectedItems();
      if (!selected.length) return;
      archiveBtn.disabled = true;
      deleteBtn.disabled = true;
      archiveBtn.textContent = "Archiving…";
      try {
        for (const item of selected) {
          // Archive: append to ARCHIVE.md
          const archiveBlock = item.block || `### Entry: ${item.title}\n`;
          await gh.commitFile({
            path: "ARCHIVE.md",
            append: archiveBlock + "\n---\n",
            message: `team-memory: archive "${item.title}"`,
          });
          // Remove from source file
          const srcFile = await gh.getFile(item.file);
          if (srcFile.exists) {
            const newContent = removeEntryFromContent(srcFile.content, item.title);
            await gh.putContent({
              path: item.file,
              content: newContent,
              message: `team-memory: remove stale "${item.title}" from ${item.file}`,
            });
          }
          await cache.delete("file:" + item.file);
        }
        toast(`Archived ${selected.length} entry/entries.`);
        renderStale(root, { config, toast });
      } catch (e) {
        toast("Archive failed: " + e.message, true);
        archiveBtn.disabled = false;
        deleteBtn.disabled = false;
        archiveBtn.textContent = "Archive selected";
      }
    };

    deleteBtn.onclick = async () => {
      const selected = getSelectedItems();
      if (!selected.length) return;
      archiveBtn.disabled = true;
      deleteBtn.disabled = true;
      deleteBtn.textContent = "Deleting…";
      try {
        // Group by file
        const byFile = {};
        for (const item of selected) {
          if (!byFile[item.file]) byFile[item.file] = [];
          byFile[item.file].push(item.title);
        }
        for (const [file, titles] of Object.entries(byFile)) {
          const srcFile = await gh.getFile(file);
          if (srcFile.exists) {
            let content = srcFile.content;
            for (const title of titles) {
              content = removeEntryFromContent(content, title);
            }
            await gh.putContent({
              path: file,
              content,
              message: `team-memory: delete ${titles.length} stale entry/entries from ${file}`,
            });
            await cache.delete("file:" + file);
          }
        }
        toast(`Deleted ${selected.length} entry/entries.`);
        renderStale(root, { config, toast });
      } catch (e) {
        toast("Delete failed: " + e.message, true);
        archiveBtn.disabled = false;
        deleteBtn.disabled = false;
        deleteBtn.textContent = "Delete selected";
      }
    };

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
    const blocks = cached.content.split(/(?=### Entry:)/);

    for (const block of blocks) {
      if (!block.startsWith("### Entry:")) continue;
      const headerLine = block.split(/\r?\n/)[0];
      const tsMatch = headerLine.match(/(\d{4}-\d{2}-\d{2}T[\d:.Z+-]+)/);
      if (tsMatch) {
        const age = Math.floor((now - new Date(tsMatch[1]).getTime()) / (24 * 60 * 60 * 1000));
        if (age > STALE_DAYS) {
          const title = headerLine.replace(/^### Entry:\s*/, "").replace(/\s*\|.*$/, "").trim();
          stale.push({ title, file: entry.path, age, block });
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
