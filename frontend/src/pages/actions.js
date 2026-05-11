export function formatAction({ text, priority, owner, due }) {
  let line = `- [ ] ${priority} | ${text}`;
  if (owner.trim()) line += ` | Owner: ${owner.trim()}`;
  if (due) line += ` | Due: ${due}`;
  return line + '\n';
}

export function toggleActionLine(content, line) {
  if (line.startsWith('- [ ]')) {
    return content.replace(line, line.replace('- [ ]', '- [x]'));
  }
  return content.replace(line, line.replace('- [x]', '- [ ]'));
}

function parseActions(content) {
  return content.split('\n')
    .filter(l => l.startsWith('- [ ]') || l.startsWith('- [x]'))
    .map(l => ({ line: l, done: l.startsWith('- [x]'), text: l.replace(/^- \[.\] /, '') }));
}

export function renderActions(container, config, gh) {
  async function load() {
    container.innerHTML = `<div class="card"><p class="muted">Loading actions…</p></div>`;
    let existingContent = '';
    try {
      const f = await gh.getFile('ACTIONS.md');
      if (f.exists) existingContent = f.content;
    } catch {}
    render(existingContent);
  }

  function render(existingContent) {
    const actions = parseActions(existingContent);
    const listHtml = actions.length === 0
      ? '<p class="muted" style="text-align:center;padding:16px">No actions yet.</p>'
      : actions.map((a, i) => `
        <div style="display:flex;align-items:center;gap:10px;padding:8px 0;border-bottom:1px solid var(--border,#eee)">
          <input type="checkbox" data-idx="${i}" data-line="${encodeURIComponent(a.line)}" ${a.done ? 'checked' : ''} style="accent-color:var(--accent,#c84e1a)">
          <span style="${a.done ? 'text-decoration:line-through;color:var(--muted)' : ''}">${a.text}</span>
        </div>`).join('');

    container.innerHTML = `
      <div class="card">
        <div id="actions-list">${listHtml}</div>
        <div class="divider" style="margin:16px 0"></div>
        <p class="section-label">Add action item</p>
        <input id="atext" class="search-input" type="text" placeholder="What needs to be done?" style="width:100%;box-sizing:border-box;margin-bottom:8px">
        <div class="fields-row">
          <div>
            <p class="section-label">Priority</p>
            <select id="apriority" class="search-input" style="width:100%;box-sizing:border-box">
              <option>HIGH</option>
              <option selected>MEDIUM</option>
              <option>LOW</option>
            </select>
          </div>
          <div>
            <p class="section-label">Owner (optional)</p>
            <input id="aowner" class="search-input" type="text" placeholder="Alice" style="width:100%;box-sizing:border-box">
          </div>
        </div>
        <div style="margin-top:8px">
          <p class="section-label">Due date (optional)</p>
          <input id="adue" class="search-input" type="date" style="width:200px">
        </div>
        <div class="divider"></div>
        <div class="button-row">
          <button id="asave" class="btn-primary">Add action</button>
        </div>
        <div id="astatus"></div>
      </div>`;

    document.querySelectorAll('#actions-list input[type=checkbox]').forEach(cb => {
      cb.addEventListener('change', async () => {
        const line = decodeURIComponent(cb.dataset.line);
        cb.disabled = true;
        try {
          const f = await gh.getFile('ACTIONS.md');
          const content = f.exists ? f.content : '';
          const newContent = toggleActionLine(content, line);
          await gh.putContent({ path: 'ACTIONS.md', content: newContent, message: 'feat: toggle action' });
          load();
        } catch (e) {
          cb.checked = !cb.checked;
          cb.disabled = false;
        }
      });
    });

    document.getElementById('asave').onclick = async () => {
      const text = document.getElementById('atext').value.trim();
      if (!text) return;
      const md = formatAction({
        text,
        priority: document.getElementById('apriority').value,
        owner: document.getElementById('aowner').value,
        due: document.getElementById('adue').value,
      });
      document.getElementById('asave').disabled = true;
      document.getElementById('asave').textContent = 'Saving…';
      try {
        await gh.commitFile({ path: 'ACTIONS.md', append: md, message: `feat: add action — ${text.slice(0, 40)}` });
        load();
      } catch (e) {
        document.getElementById('astatus').innerHTML = `<p style="color:var(--error)">${e.message}</p>`;
        document.getElementById('asave').disabled = false;
        document.getElementById('asave').textContent = 'Add action';
      }
    };
  }

  load();
}
