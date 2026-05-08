export function extractEntries(content, source) {
  const blocks = content.split(/(?=### Entry:)/);
  return blocks
    .filter(b => b.startsWith('### Entry:'))
    .map(b => {
      const titleMatch = b.match(/^### Entry:\s*(.+)/);
      const dateMatch = b.match(/\*\*Date:\*\*\s*(\d{4}-\d{2}-\d{2})/);
      const summaryMatch = b.match(/\*\*Summary:\*\*\s*(.+)/);
      const tagsMatch = b.match(/\*\*Tags:\*\*\s*(.+)/);
      if (!titleMatch || !dateMatch) return null;
      return {
        title: titleMatch[1].trim(),
        date: dateMatch[1],
        summary: summaryMatch ? summaryMatch[1].trim() : '',
        tags: tagsMatch ? tagsMatch[1].trim().split(';').filter(Boolean) : [],
        source,
      };
    })
    .filter(Boolean);
}

export function sortByDate(entries) {
  return [...entries].sort((a, b) => b.date.localeCompare(a.date));
}

export function renderTimeline(container, config, gh) {
  container.innerHTML = `<div class="card"><p class="muted" style="text-align:center;padding:24px">Loading timeline…</p></div>`;

  (async () => {
    try {
      const indexFile = await gh.getFile('INDEX.md');
      if (!indexFile.exists) {
        container.innerHTML = `<div class="card"><p class="muted" style="text-align:center;padding:24px">No INDEX.md found. Save some entries first.</p></div>`;
        return;
      }
      const indexContent = indexFile.content;
      const fileMatches = [...indexContent.matchAll(/`([A-Z][A-Z0-9_/.-]+\.md)`/g)];
      const files = [...new Set(fileMatches.map(m => m[1]))];

      const allEntries = [];
      await Promise.all(files.map(async f => {
        try {
          const file = await gh.getFile(f);
          if (file.exists) {
            extractEntries(file.content, f).forEach(e => allEntries.push(e));
          }
        } catch {}
      }));

      const sorted = sortByDate(allEntries);

      if (sorted.length === 0) {
        container.innerHTML = `<div class="card"><p class="muted" style="text-align:center;padding:24px">No dated entries found.</p></div>`;
        return;
      }

      let lastDate = '';
      const items = sorted.map(e => {
        let dateHeader = '';
        if (e.date !== lastDate) {
          lastDate = e.date;
          dateHeader = `<p class="section-label" style="margin:16px 0 8px">${e.date}</p>`;
        }
        const tags = e.tags.map(t =>
          `<span style="font-size:11px;background:var(--bg-alt,#f5ede6);padding:2px 7px;border-radius:10px;color:var(--primary,#c0541a)">${t}</span>`
        ).join(' ');
        return `${dateHeader}
          <div style="padding:12px;border:1px solid var(--border,#e8ddd4);border-radius:8px;margin-bottom:8px">
            <div style="display:flex;justify-content:space-between;align-items:start;gap:8px">
              <strong style="font-size:14px">${e.title}</strong>
              <span class="muted" style="font-size:11px;white-space:nowrap">${e.source}</span>
            </div>
            ${e.summary ? `<p style="margin:4px 0;font-size:13px;color:var(--text-secondary,#555)">${e.summary}</p>` : ''}
            ${tags ? `<div style="margin-top:6px;display:flex;flex-wrap:wrap;gap:4px">${tags}</div>` : ''}
          </div>`;
      }).join('');

      container.innerHTML = `
        <div class="card" style="padding-bottom:24px">
          <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:4px">
            <h3 style="margin:0;font-size:16px">Timeline</h3>
            <span class="muted">${sorted.length} entries</span>
          </div>
          ${items}
        </div>`;
    } catch (e) {
      container.innerHTML = `<div class="card"><p style="color:var(--error)">${e.message}</p></div>`;
    }
  })();
}
