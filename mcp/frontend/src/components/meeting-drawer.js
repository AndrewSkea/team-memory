import { parseMsgFile } from '../services/msgparser.js';
import { deriveSlug, parseSeries, buildSeriesFile, formatOccurrence, appendOccurrence } from '../services/meetings-series.js';
import { MEETING_TEMPLATES, applyMeetingTemplate } from '../pages/meetings.js';
import { pickBackend } from '../services/llm/backend.js';
import { GitHubClient } from '../services/github.js';
import { listTodayMeetings } from '../services/google-calendar.js';
import { refreshGoogleToken } from '../services/oauth.js';
import { postToSlack } from '../services/slack.js';

const TODAY = () => new Date().toISOString().slice(0, 10);

function escHtml(s) {
  return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

export function initMeetingDrawer(config, gh) {
  const root = document.getElementById('meeting-drawer-root');
  if (!root) return;

  root.innerHTML = `
    <button class="meeting-fab" id="mfab" title="Meeting notes">
      <svg viewBox="0 0 24 24"><rect x="3" y="4" width="18" height="18" rx="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>
    </button>
    <div class="meeting-overlay" id="moverlay"></div>
    <div class="meeting-drawer" id="mdrawer">
      <div class="meeting-drawer__header">
        <span style="display:flex;align-items:baseline;gap:0;overflow:hidden">
          <span class="meeting-drawer__title" id="mdrawer-title">New Meeting</span>
          <span class="meeting-drawer__badge" id="mdrawer-badge"></span>
        </span>
        <button class="meeting-drawer__close" id="mdrawer-close" aria-label="Close">×</button>
      </div>
      <div class="meeting-drawer__tabs">
        <button class="meeting-drawer__tab meeting-drawer__tab--active" data-drawtab="notes">Notes</button>
        <button class="meeting-drawer__tab" data-drawtab="requirements">Requirements</button>
      </div>
      <div class="meeting-drawer__body" id="mdrawer-body"></div>
    </div>
  `;

  const state = { tab: 'notes', series: null, slug: null };

  const fab     = document.getElementById('mfab');
  const overlay = document.getElementById('moverlay');
  const drawer  = document.getElementById('mdrawer');
  const titleEl = document.getElementById('mdrawer-title');
  const badgeEl = document.getElementById('mdrawer-badge');
  const body    = document.getElementById('mdrawer-body');

  function openDrawer()  { drawer.classList.add('meeting-drawer--open'); overlay.classList.add('meeting-overlay--visible'); renderBody(); }
  function closeDrawer() { drawer.classList.remove('meeting-drawer--open'); overlay.classList.remove('meeting-overlay--visible'); }

  fab.addEventListener('click', openDrawer);
  overlay.addEventListener('click', closeDrawer);
  document.getElementById('mdrawer-close').addEventListener('click', closeDrawer);

  drawer.addEventListener('click', e => {
    const tab = e.target.closest('[data-drawtab]');
    if (!tab) return;
    state.tab = tab.dataset.drawtab;
    drawer.querySelectorAll('.meeting-drawer__tab').forEach(t =>
      t.classList.toggle('meeting-drawer__tab--active', t.dataset.drawtab === state.tab)
    );
    renderBody();
  });

  async function loadSeries(slug) {
    const file = await gh.getFile(`meetings/${slug}.md`);
    state.slug = slug;
    if (!file.exists) { state.series = null; return; }
    state.series = parseSeries(file.content);
    state.series._rawContent = file.content;
    titleEl.textContent = state.series.frontmatter.title || slug;
    const count = state.series.occurrences.length;
    badgeEl.textContent = count ? ` · ${count} session${count !== 1 ? 's' : ''}` : '';
  }

  function renderBody() {
    if (state.tab === 'notes') renderNotesTab();
    else renderRequirementsTab();
  }

  function renderNotesTab() {
    const savedTitle = state.series?.frontmatter.title ?? '';
    const savedAttendees = state.series?.frontmatter.attendees ?? '';
    const historyHtml = state.series?.occurrences.length
      ? `<div class="meeting-history">
           <p class="drawer-label">Past sessions</p>
           ${state.series.occurrences.map((occ, i) => `
             <button class="meeting-history__toggle" data-histidx="${i}">
               ${escHtml(occ.date)}
               <span style="margin-left:auto;font-size:14px">›</span>
             </button>
             <div class="meeting-history__item" id="hist-item-${i}">
               ${occ.notes    ? `<p><strong>Notes</strong><br>${escHtml(occ.notes)}</p>` : ''}
               ${occ.decisions? `<p><strong>Decisions</strong><br>${escHtml(occ.decisions)}</p>` : ''}
               ${occ.actions  ? `<p><strong>Actions</strong><br>${escHtml(occ.actions)}</p>` : ''}
             </div>
           `).join('')}
         </div>`
      : '';

    body.innerHTML = `
      <div class="drop-zone" id="mdrop" style="cursor:pointer;text-align:center;padding:10px">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="18" height="18" style="display:block;margin:0 auto 4px"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
        <span style="font-size:12px;color:var(--muted)">Drop .msg invite or click to browse</span>
        <input type="file" accept=".msg" id="mdfile" style="display:none">
      </div>
      ${historyHtml}
      <div>
        <p class="drawer-label">Template</p>
        <select id="mdtmpl" class="search-input" style="width:100%;padding-left:12px">
          ${MEETING_TEMPLATES.map(t => `<option value="${escHtml(t.name)}">${escHtml(t.name)}</option>`).join('')}
        </select>
      </div>
      <div>
        <p class="drawer-label">Title</p>
        <input id="mdtitle" class="search-input" type="text" placeholder="Sprint Planning" style="width:100%;padding-left:12px" value="${escHtml(savedTitle)}">
      </div>
      <div style="display:flex;gap:8px">
        <div style="flex:1">
          <p class="drawer-label">Date</p>
          <input id="mddate" class="search-input" type="date" value="${TODAY()}" style="width:100%;padding-left:12px">
        </div>
        <div style="flex:2">
          <p class="drawer-label">Attendees</p>
          <input id="mdattendees" class="search-input" type="text" placeholder="Alice, Bob" value="${escHtml(savedAttendees)}" style="width:100%;padding-left:12px">
        </div>
      </div>
      <div>
        <p class="drawer-label">Notes</p>
        <textarea id="mdnotes" class="textarea" style="min-height:80px" placeholder="Key discussion points..."></textarea>
      </div>
      <div>
        <p class="drawer-label">Decisions</p>
        <textarea id="mddecisions" class="textarea" style="min-height:60px" placeholder="We decided to..."></textarea>
      </div>
      <div>
        <p class="drawer-label">Action Items (Owner: task)</p>
        <textarea id="mdactions" class="textarea" style="min-height:60px" placeholder="Alice: set up board"></textarea>
      </div>
      <div id="mdenrich-status"></div>
      <div style="display:flex;gap:8px">
        <button class="btn-primary" id="mdsave" style="flex:1">Save</button>
        <button class="btn-outline" id="mdenrich" style="flex:1">Enrich with AI</button>
      </div>
      <div id="mdsave-status"></div>
    `;

    // Google Calendar "Today's meetings" dropdown
    if (config.googleAccessToken) {
      const calDiv = document.createElement('div');
      calDiv.style.cssText = 'margin-bottom:8px';
      calDiv.innerHTML = `<p class="drawer-label" style="margin-bottom:4px">Today's meetings</p>
        <select id="gcal-select" class="search-input" style="width:100%;padding-left:12px">
          <option value="">Loading…</option>
        </select>`;
      const dropzone = document.getElementById('mdrop');
      if (dropzone) body.insertBefore(calDiv, dropzone);

      listTodayMeetings(
        config.googleAccessToken,
        async () => {
          const newToken = await refreshGoogleToken(config.googleClientId, config.googleRefreshToken);
          if (newToken) {
            config.googleAccessToken = newToken;
            const saved = JSON.parse(localStorage.getItem('team-memory:config') || '{}');
            saved.googleAccessToken = newToken;
            localStorage.setItem('team-memory:config', JSON.stringify(saved));
          }
          return newToken;
        }
      ).then(events => {
        const sel = document.getElementById('gcal-select');
        if (!sel) return;
        if (!events.length) {
          sel.innerHTML = '<option value="">No meetings today</option>';
          return;
        }
        sel.innerHTML = '<option value="">Pick a meeting…</option>' +
          events.map((e, i) => `<option value="${i}">${escHtml(e.title)}</option>`).join('');
        sel.addEventListener('change', () => {
          const idx = parseInt(sel.value, 10);
          if (isNaN(idx)) return;
          const ev = events[idx];
          const titleInput = document.getElementById('mdtitle');
          const dateInput = document.getElementById('mddate');
          const attendeesInput = document.getElementById('mdattendees');
          if (titleInput) titleInput.value = ev.title;
          if (dateInput) dateInput.value = ev.start.slice(0, 10);
          if (attendeesInput) attendeesInput.value = ev.attendees.join(', ');
        });
      }).catch(() => {
        const sel = document.getElementById('gcal-select');
        if (sel) sel.innerHTML = '<option value="">Could not load meetings</option>';
      });
    }

    document.getElementById('mdrop').addEventListener('click', () => document.getElementById('mdfile').click());
    document.getElementById('mdrop').addEventListener('dragover', e => e.preventDefault());
    document.getElementById('mdrop').addEventListener('drop', e => { e.preventDefault(); handleMsgFile(e.dataTransfer.files[0]); });
    document.getElementById('mdfile').addEventListener('change', e => handleMsgFile(e.target.files[0]));

    document.getElementById('mdtmpl').addEventListener('change', e => {
      const f = applyMeetingTemplate(e.target.value);
      if (f.title) document.getElementById('mdtitle').value = f.title;
      document.getElementById('mddecisions').value = f.decisions;
      document.getElementById('mdactions').value = f.actions;
    });

    document.getElementById('mdtitle').addEventListener('blur', async e => {
      const title = e.target.value.trim();
      if (!title) return;
      const slug = deriveSlug(title);
      if (slug === state.slug) return;
      await loadSeries(slug);
      renderBody();
      // restore title since renderBody resets it
      const titleInput = document.getElementById('mdtitle');
      if (titleInput) titleInput.value = title;
    });

    body.querySelectorAll('[data-histidx]').forEach(btn => {
      btn.addEventListener('click', () => {
        const i = btn.dataset.histidx;
        const item = document.getElementById(`hist-item-${i}`);
        if (item) item.classList.toggle('meeting-history__item--open');
      });
    });

    document.getElementById('mdsave').addEventListener('click', handleSave);
    document.getElementById('mdenrich').addEventListener('click', handleEnrich);
  }

  function renderRequirementsTab() {
    const occurrences = state.series?.occurrences ?? [];
    body.innerHTML = `
      <p class="drawer-label">Select sessions to include</p>
      <div class="scope-row" id="mdscope">
        ${occurrences.length
          ? occurrences.map((occ, i) =>
              `<label class="scope-check"><input type="checkbox" data-occidx="${i}" checked> ${escHtml(occ.date)}</label>`
            ).join('')
          : '<span style="font-size:12px;color:var(--muted)">Save a meeting first to generate requirements.</span>'
        }
      </div>
      <button class="btn-primary" id="mdgenreq" style="width:100%"${!occurrences.length ? ' disabled' : ''}>Generate Requirements</button>
      <div id="mdreq-output" class="requirements-output" style="display:none;margin-top:8px"></div>
      <button class="btn-outline" id="mdreq-save" style="width:100%;display:none">Save to repo</button>
      <div id="mdreq-status"></div>
    `;

    document.getElementById('mdgenreq')?.addEventListener('click', handleGenerateRequirements);
    document.getElementById('mdreq-save')?.addEventListener('click', handleSaveRequirements);
  }

  async function handleMsgFile(file) {
    if (!file?.name.endsWith('.msg')) return;
    try {
      const buf = await file.arrayBuffer();
      const { subject, date, displayTo, displayCc } = parseMsgFile(buf);
      if (subject) {
        const slug = deriveSlug(subject);
        await loadSeries(slug);
        renderBody();
        const titleInput = document.getElementById('mdtitle');
        if (titleInput) titleInput.value = state.series?.frontmatter.title ?? subject;
        if (date) { const d = document.getElementById('mddate'); if (d) d.value = date.toISOString().slice(0, 10); }
        const att = [displayTo, displayCc].filter(Boolean).join(', ');
        if (att) { const a = document.getElementById('mdattendees'); if (a) a.value = att; }
      }
    } catch {
      const s = document.getElementById('mdenrich-status');
      if (s) s.innerHTML = '<p style="color:var(--danger);font-size:12px">Could not parse .msg file.</p>';
    }
  }

  async function handleEnrich() {
    const btn = document.getElementById('mdenrich');
    const statusEl = document.getElementById('mdenrich-status');
    btn.disabled = true;
    btn.textContent = 'Enriching…';
    statusEl.innerHTML = '';
    try {
      const llm = await pickBackend(config);
      if (!llm) throw new Error('No AI backend — add Anthropic API key in Setup.');
      const indexFile = await gh.getFile('INDEX.md');
      let codebaseContent = '';
      if (config.codebaseOwner && config.codebaseRepo) {
        const cbGh = new GitHubClient({ token: config.token, owner: config.codebaseOwner, repo: config.codebaseRepo });
        const readme = await cbGh.getFile('README.md').catch(() => ({ content: '' }));
        codebaseContent = readme.content ?? '';
      }
      const enriched = await llm.enrich({
        currentNotes: document.getElementById('mdnotes')?.value ?? '',
        seriesContent: state.series?._rawContent ?? '',
        indexContent: indexFile.content ?? '',
        codebaseContent,
      });
      const notesEl = document.getElementById('mdnotes');
      if (notesEl) notesEl.value = enriched;
      statusEl.innerHTML = '<p style="color:var(--green);font-size:12px">Notes enriched — review before saving.</p>';
    } catch (e) {
      statusEl.innerHTML = `<p style="color:var(--danger);font-size:12px">${escHtml(e.message)}</p>`;
    } finally {
      btn.disabled = false;
      btn.textContent = 'Enrich with AI';
    }
  }

  async function handleSave() {
    const btn = document.getElementById('mdsave');
    const statusEl = document.getElementById('mdsave-status');
    const title = document.getElementById('mdtitle')?.value.trim() ?? '';
    if (!title) { statusEl.innerHTML = '<p style="color:var(--danger);font-size:12px">Title required.</p>'; return; }
    btn.disabled = true;
    btn.textContent = 'Saving…';
    statusEl.innerHTML = '';
    const slug = state.slug ?? deriveSlug(title);
    const occurrence = {
      date:       document.getElementById('mddate')?.value ?? TODAY(),
      notes:      document.getElementById('mdnotes')?.value ?? '',
      decisions:  document.getElementById('mddecisions')?.value ?? '',
      actions:    document.getElementById('mdactions')?.value ?? '',
      enrichment: '',
    };
    let content;
    if (state.series) {
      content = appendOccurrence(state.series._rawContent, occurrence);
    } else {
      const attendees = document.getElementById('mdattendees')?.value.trim() ?? '';
      const template  = document.getElementById('mdtmpl')?.value ?? 'Blank';
      content = buildSeriesFile({ title, slug, attendees, template }) + formatOccurrence(occurrence);
    }
    try {
      const r = await gh.putContent({ path: `meetings/${slug}.md`, content, message: `feat: meeting notes — ${title} ${occurrence.date}` });
      if (!r.ok) throw new Error(r.message ?? 'Save failed');
      statusEl.innerHTML = '<p style="color:var(--green);font-size:12px">Saved.</p>';
      if (config.slackWebhookUrl) {
        const slackBtn = document.createElement('button');
        slackBtn.className = 'btn-outline';
        slackBtn.style.cssText = 'margin-top:6px;font-size:12px;width:100%';
        slackBtn.textContent = 'Post to Slack';
        statusEl.appendChild(slackBtn);
        const notesVal = document.getElementById('mdnotes')?.value ?? '';
        const actionsVal = document.getElementById('mdactions')?.value ?? '';
        slackBtn.addEventListener('click', async () => {
          const text = `*Meeting saved: ${title} — ${occurrence.date}*\nNotes: ${notesVal}\nActions: ${actionsVal}`;
          try {
            await postToSlack(config.slackWebhookUrl, text);
            slackBtn.textContent = 'Posted ✓';
            setTimeout(() => slackBtn.remove(), 2000);
          } catch (e) {
            slackBtn.textContent = 'Post failed: ' + e.message;
          }
        });
      }
      await loadSeries(slug);
      renderBody();
    } catch (e) {
      statusEl.innerHTML = `<p style="color:var(--danger);font-size:12px">${escHtml(e.message)}</p>`;
    } finally {
      btn.disabled = false;
      btn.textContent = 'Save';
    }
  }

  async function handleGenerateRequirements() {
    const btn = document.getElementById('mdgenreq');
    const outputEl = document.getElementById('mdreq-output');
    const statusEl = document.getElementById('mdreq-status');
    btn.disabled = true;
    btn.textContent = 'Generating…';
    outputEl.style.display = '';
    outputEl.textContent = 'Working…';
    statusEl.innerHTML = '';
    try {
      const llm = await pickBackend(config);
      if (!llm) throw new Error('No AI backend — add Anthropic API key in Setup.');
      const checked = [...document.querySelectorAll('#mdscope input[type=checkbox]:checked')];
      if (!checked.length) throw new Error('Select at least one session.');
      if (!state.series) throw new Error('No series loaded. Save a meeting first.');
      const selected = checked.map(cb => state.series.occurrences[parseInt(cb.dataset.occidx)]);
      const occurrencesContent = selected.map(o =>
        `## ${o.date}\nNotes: ${o.notes}\nDecisions: ${o.decisions}\nActions: ${o.actions}`
      ).join('\n\n');
      const indexFile = await gh.getFile('INDEX.md');
      let codebaseContent = '';
      if (config.codebaseOwner && config.codebaseRepo) {
        const cbGh = new GitHubClient({ token: config.token, owner: config.codebaseOwner, repo: config.codebaseRepo });
        const readme = await cbGh.getFile('README.md').catch(() => ({ content: '' }));
        codebaseContent = readme.content ?? '';
      }
      const result = await llm.generateRequirements({ occurrencesContent, indexContent: indexFile.content ?? '', codebaseContent });
      outputEl.textContent = result;
      const saveBtn = document.getElementById('mdreq-save');
      if (saveBtn) { saveBtn.style.display = ''; saveBtn.dataset.content = result; }
    } catch (e) {
      outputEl.style.display = 'none';
      statusEl.innerHTML = `<p style="color:var(--danger);font-size:12px">${escHtml(e.message)}</p>`;
    } finally {
      btn.disabled = false;
      btn.textContent = 'Generate Requirements';
    }
  }

  async function handleSaveRequirements() {
    const btn = document.getElementById('mdreq-save');
    const statusEl = document.getElementById('mdreq-status');
    const content = btn.dataset.content;
    if (!content || !state.slug) return;
    btn.disabled = true;
    btn.textContent = 'Saving…';
    const today = TODAY();
    const title = state.series?.frontmatter.title ?? state.slug;
    const path = `requirements/${today}-${state.slug}-requirements.md`;
    const header = `# Requirements: ${title} — ${today}\n\n`;
    try {
      const r = await gh.putContent({ path, content: header + content, message: `feat: requirements — ${state.slug} ${today}` });
      if (!r.ok) throw new Error(r.message ?? 'Save failed');
      statusEl.innerHTML = `<p style="color:var(--green);font-size:12px">Saved to <code>${escHtml(path)}</code></p>`;
      if (config.slackWebhookUrl) {
        const slackBtn = document.createElement('button');
        slackBtn.className = 'btn-outline';
        slackBtn.style.cssText = 'margin-top:6px;font-size:12px;width:100%';
        slackBtn.textContent = 'Post to Slack';
        statusEl.appendChild(slackBtn);
        const snippet = content.slice(0, 500);
        const date = TODAY();
        slackBtn.addEventListener('click', async () => {
          const text = `*Requirements generated: ${state.slug} — ${date}*\n${snippet}${content.length > 500 ? '…' : ''}`;
          try {
            await postToSlack(config.slackWebhookUrl, text);
            slackBtn.textContent = 'Posted ✓';
            setTimeout(() => slackBtn.remove(), 2000);
          } catch (e) {
            slackBtn.textContent = 'Post failed: ' + e.message;
          }
        });
      }
    } catch (e) {
      statusEl.innerHTML = `<p style="color:var(--danger);font-size:12px">${escHtml(e.message)}</p>`;
    } finally {
      btn.disabled = false;
      btn.textContent = 'Save to repo';
    }
  }
}
