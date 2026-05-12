import { parseMsgFile } from "../services/msgparser.js";
import { pickBackend } from "../services/llm/backend.js";

const FILE = owner => `users/${owner}/REMINDERS.md`;

export function parseReminders(md) {
  return md
    .split(/^---$/m)
    .map(s => s.trim())
    .filter(Boolean)
    .map(block => {
      const lines = block.split('\n');
      const headLine = lines[0] ?? '';
      const doneMatch = headLine.match(/^###\s+~~(.+?)~~\s+\[DONE\]$/);
      const plainMatch = headLine.match(/^###\s+(.+)$/);
      const title = doneMatch ? doneMatch[1] : (plainMatch ? plainMatch[1] : '');
      const done  = !!doneMatch;
      const dueLine  = lines.find(l => l.startsWith('**Due:**'));
      const tagLine  = lines.find(l => l.startsWith('**Tags:**'));
      const dueDate  = dueLine  ? dueLine.replace('**Due:**', '').trim() : '';
      const tags     = tagLine  ? tagLine.replace('**Tags:**', '').trim() : '';
      const bullets  = lines.filter(l => l.startsWith('- ')).map(l => l.slice(2));
      return { title, dueDate, done, tags, bullets };
    })
    .filter(r => r.title);
}

export function markDone(md, title) {
  const escaped = title.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const re = new RegExp(`^(###\\s+)(${escaped})(\\s*)$`, 'm');
  if (!re.test(md)) return md;
  return md.replace(re, `$1~~$2~~ [DONE]$3`);
}

export function formatReminderEntry({ short_title, bullets, tags }, dueDate) {
  const lines = [`### ${short_title}`, `**Due:** ${dueDate}`];
  if (tags) lines.push(`**Tags:** ${tags}`);
  for (const b of (bullets ?? [])) lines.push(`- ${b}`);
  lines.push('---');
  return lines.join('\n') + '\n';
}

export function renderReminders(root, config, gh) {
  let activeTab = 'add';

  function render() {
    root.innerHTML = `
      <div class="card">
        <div class="tab-row">
          <button class="tab-btn${activeTab === 'add' ? ' active' : ''}" data-tab="add">Add reminder</button>
          <button class="tab-btn${activeTab === 'view' ? ' active' : ''}" data-tab="view">View reminders</button>
        </div>
        <div id="tab-content"></div>
      </div>`;

    root.querySelector('.tab-row').addEventListener('click', e => {
      const b = e.target.closest('[data-tab]');
      if (b && b.dataset.tab !== activeTab) { activeTab = b.dataset.tab; render(); }
    });

    if (activeTab === 'add') renderAdd();
    else renderView();
  }

  function renderAdd() {
    const today = new Date().toISOString().slice(0, 10);
    document.getElementById('tab-content').innerHTML = `
      <div class="drop-zone" id="rdrop">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="20" height="20" style="display:block;margin:0 auto 6px"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
        Drop an email (.msg) to auto-fill, or fill in manually
        <input type="file" accept=".msg" id="rfile">
      </div>
      <div>
        <p class="section-label">What do you need to do?</p>
        <input id="rtitle" class="search-input" type="text" placeholder="Submit Q2 report" style="width:100%;box-sizing:border-box">
      </div>
      <div>
        <p class="section-label">Due date</p>
        <input id="rdate" class="search-input" type="date" value="${today}" style="width:100%;box-sizing:border-box">
      </div>
      <div>
        <p class="section-label">Details <span style="color:var(--muted);font-weight:400">(optional)</span></p>
        <textarea id="rdetails" placeholder="Context, links, notes…" style="min-height:100px"></textarea>
      </div>
      <div class="divider"></div>
      <div class="button-row">
        <button class="btn-primary" id="rsave">
          <svg viewBox="0 0 16 16"><path d="M14 2H9l-7 7 5 5 7-7V2z"/><circle cx="12" cy="4" r="1.2" fill="currentColor" stroke="none"/></svg>
          Save reminder
        </button>
      </div>
      <div id="rstatus"></div>`;

    function handleMsg(file) {
      if (!file || !file.name.endsWith('.msg')) return;
      file.arrayBuffer().then(buf => {
        try {
          const { subject, date, body } = parseMsgFile(buf);
          if (subject) document.getElementById('rtitle').value = subject;
          if (date) document.getElementById('rdate').value = date.toISOString().slice(0, 10);
          if (body) document.getElementById('rdetails').value = body.trim().slice(0, 1000);
        } catch {
          document.getElementById('rstatus').innerHTML = '<p style="color:var(--danger)">Could not parse .msg file</p>';
        }
      });
    }

    const drop = document.getElementById('rdrop');
    drop.addEventListener('dragover', e => { e.preventDefault(); drop.classList.add('drag-over'); });
    drop.addEventListener('dragleave', () => drop.classList.remove('drag-over'));
    drop.addEventListener('drop', e => { e.preventDefault(); drop.classList.remove('drag-over'); handleMsg(e.dataTransfer.files[0]); });
    document.getElementById('rfile').addEventListener('change', e => handleMsg(e.target.files[0]));

    document.getElementById('rsave').addEventListener('click', async () => {
      const title   = document.getElementById('rtitle').value.trim();
      const dueDate = document.getElementById('rdate').value;
      const details = document.getElementById('rdetails').value.trim();
      if (!title)   { document.getElementById('rstatus').innerHTML = '<p style="color:var(--danger)">Title required</p>'; return; }
      if (!dueDate) { document.getElementById('rstatus').innerHTML = '<p style="color:var(--danger)">Due date required</p>'; return; }

      const btn = document.getElementById('rsave');
      btn.disabled = true;
      btn.innerHTML = `<svg viewBox="0 0 16 16"><path d="M14 2H9l-7 7 5 5 7-7V2z"/><circle cx="12" cy="4" r="1.2" fill="currentColor" stroke="none"/></svg> Saving…`;

      try {
        const backend = await pickBackend({ anthropicKey: config.anthropicKey });
        let formatted;
        if (backend) {
          formatted = await backend.formatReminder({ title, dueDate, details });
        } else {
          formatted = { short_title: title.slice(0, 60), bullets: details ? [details.split('\n')[0].slice(0, 80)] : [], tags: '' };
        }
        const entry = formatReminderEntry(formatted, dueDate);
        await gh.commitFile({ path: FILE(config.owner), append: '\n' + entry, message: `team-memory: add reminder — ${formatted.short_title}` });
        document.getElementById('rstatus').innerHTML = '<p style="color:var(--green)">Saved!</p>';
        ['rtitle', 'rdetails'].forEach(id => { document.getElementById(id).value = ''; });
        document.getElementById('rdate').value = new Date().toISOString().slice(0, 10);
      } catch (e) {
        document.getElementById('rstatus').innerHTML = `<p style="color:var(--danger)">${e.message}</p>`;
      } finally {
        btn.disabled = false;
        btn.innerHTML = `<svg viewBox="0 0 16 16"><path d="M14 2H9l-7 7 5 5 7-7V2z"/><circle cx="12" cy="4" r="1.2" fill="currentColor" stroke="none"/></svg> Save reminder`;
      }
    });
  }

  function renderView() {
    document.getElementById('tab-content').innerHTML = '<p style="color:var(--muted);text-align:center;padding:24px">Loading…</p>';
    loadAndRenderView();
  }

  async function loadAndRenderView() {
    const content = document.getElementById('tab-content');
    try {
      const file = await gh.getFile(FILE(config.owner));
      const items = parseReminders(file?.content ?? '');
      const now = new Date();
      const weekAhead = new Date(now.getTime() + 7 * 86400000);

      function dateOf(s) { return s ? new Date(s + 'T00:00:00') : null; }

      const overdue   = items.filter(r => !r.done && dateOf(r.dueDate) < now);
      const thisWeek  = items.filter(r => !r.done && dateOf(r.dueDate) >= now && dateOf(r.dueDate) <= weekAhead);
      const upcoming  = items.filter(r => !r.done && dateOf(r.dueDate) > weekAhead);
      const done      = items.filter(r => r.done);

      function renderItem(r) {
        const isOverdue = dateOf(r.dueDate) < now && !r.done;
        return `<div class="reminder-item${isOverdue ? ' overdue' : ''}">
          <div class="reminder-header">
            <span class="reminder-title">${r.title}</span>
            <span class="reminder-due${isOverdue ? ' overdue' : ''}">${r.dueDate}</span>
          </div>
          ${r.bullets[0] ? `<div class="reminder-preview">${r.bullets[0]}</div>` : ''}
          ${!r.done ? `<button class="reminder-done-btn" data-title="${r.title.replace(/"/g, '&quot;')}">Mark done</button>` : ''}
        </div>`;
      }

      function section(label, arr, accent) {
        if (!arr.length) return '';
        return `<div class="reminder-group">
          <div class="result-label" style="${accent ? 'color:var(--danger)' : ''}">${label}</div>
          ${arr.map(renderItem).join('')}
        </div>`;
      }

      const doneSectionId = 'done-section';
      content.innerHTML = `
        ${!overdue.length && !thisWeek.length && !upcoming.length && !done.length
          ? '<p style="color:var(--muted);text-align:center;padding:24px">No reminders yet. Add one!</p>'
          : ''}
        ${section('Overdue', overdue.sort((a,b) => a.dueDate.localeCompare(b.dueDate)), true)}
        ${section('This week', thisWeek.sort((a,b) => a.dueDate.localeCompare(b.dueDate)), false)}
        ${section('Upcoming', upcoming.sort((a,b) => a.dueDate.localeCompare(b.dueDate)), false)}
        ${done.length ? `
          <div class="reminder-group">
            <button class="tab-btn" id="toggle-done" style="font-size:12px;padding:4px 0">
              Done (${done.length}) ▾
            </button>
            <div id="${doneSectionId}" style="display:none">${done.map(renderItem).join('')}</div>
          </div>` : ''}`;

      document.getElementById('toggle-done')?.addEventListener('click', () => {
        const el = document.getElementById(doneSectionId);
        el.style.display = el.style.display === 'none' ? 'block' : 'none';
      });

      content.querySelectorAll('.reminder-done-btn').forEach(btn => {
        btn.addEventListener('click', async () => {
          btn.disabled = true;
          btn.textContent = 'Saving…';
          try {
            const fresh = await gh.getFile(FILE(config.owner));
            const updated = markDone(fresh.content, btn.dataset.title);
            await gh.putContent({ path: FILE(config.owner), content: updated, sha: fresh.sha, message: `team-memory: mark done — ${btn.dataset.title}` });
            renderView();
          } catch (e) {
            btn.textContent = 'Error — retry';
            btn.disabled = false;
          }
        });
      });

    } catch (e) {
      content.innerHTML = `<p style="color:var(--danger)">${e.message}</p>`;
    }
  }

  render();
}
