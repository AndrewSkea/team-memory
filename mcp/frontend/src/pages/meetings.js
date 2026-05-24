import { parseMsgFile } from '../services/msgparser.js';
import { parseSeries } from '../services/meetings-series.js';

export const MEETING_TEMPLATES = [
  { name: 'Blank',            fill: { title: '',                     decisions: '',                                      actions: '' } },
  { name: 'Sprint Planning',  fill: { title: 'Sprint Planning',      decisions: 'Sprint goal: \nSelected items: ',       actions: 'Scrum master: \nTeam: ' } },
  { name: '1:1',              fill: { title: '1:1 Check-in',         decisions: 'Status update: \nFeedback: ',           actions: '' } },
  { name: 'Retrospective',    fill: { title: 'Sprint Retrospective',  decisions: 'Went well: \nTo improve: ',             actions: '' } },
  { name: 'Incident Review',  fill: { title: 'Incident Review',      decisions: 'Root cause: \nTimeline: \nImpact: ',    actions: 'Owner: fix\nOwner: monitoring' } },
];

export function applyMeetingTemplate(name) {
  return (MEETING_TEMPLATES.find(t => t.name === name) ?? MEETING_TEMPLATES[0]).fill;
}

export function formatMeeting({ title, date, attendees, decisions, actionItems }) {
  const lines = [`### Meeting: ${title}`, `**Date:** ${date}`];
  if (attendees.trim()) lines.push(`**Attendees:** ${attendees.trim()}`);
  lines.push('');
  if (decisions.trim()) {
    lines.push('**Decisions:**');
    decisions.trim().split('\n').forEach(d => lines.push(`- ${d.trim()}`));
    lines.push('');
  }
  if (actionItems.trim()) {
    lines.push('**Action Items:**');
    actionItems.trim().split('\n').forEach(a => lines.push(`- [ ] ${a.trim()}`));
    lines.push('');
  }
  lines.push('---');
  return lines.join('\n') + '\n';
}

export function renderMeetings(container, config, gh) {
  container.innerHTML = `
    <div class="card">
      <p class="section-label">Meeting Series</p>
      <div id="mseries-list"><p style="color:var(--muted);font-size:13px">Loading…</p></div>
      <div id="mseries-detail" style="display:none"></div>
    </div>`;

  loadSeriesList(gh);
}

async function loadSeriesList(gh) {
  const listEl = document.getElementById('mseries-list');
  if (!listEl) return;
  try {
    const files = await gh.getDir('meetings');
    const mdFiles = files.filter(f => f.name.endsWith('.md'));
    if (!mdFiles.length) {
      listEl.innerHTML = '<p style="color:var(--muted);font-size:13px">No meeting series yet — use the calendar button to add one.</p>';
      return;
    }
    const rows = await Promise.all(
      mdFiles.map(async f => {
        const file = await gh.getFile(`meetings/${f.name}`);
        const { frontmatter, occurrences } = parseSeries(file.content);
        return { slug: f.name.replace('.md', ''), frontmatter, occurrences, rawContent: file.content };
      })
    );
    listEl.innerHTML = rows.map(r => `
      <div class="result-item" style="cursor:pointer" data-slug="${escHtml(r.slug)}">
        <div style="display:flex;justify-content:space-between;align-items:baseline">
          <span style="font-weight:600">${escHtml(r.frontmatter.title ?? r.slug)}</span>
          <span style="font-size:11px;color:var(--muted)">${r.occurrences.length} session${r.occurrences.length !== 1 ? 's' : ''}</span>
        </div>
        <div style="font-size:12px;color:var(--muted);margin-top:2px">
          Last: ${r.occurrences[0]?.date ?? 'none'} · Attendees: ${escHtml(r.frontmatter.attendees || '—')}
        </div>
      </div>
    `).join('');
    listEl.querySelectorAll('[data-slug]').forEach(el => {
      el.addEventListener('click', () => {
        const row = rows.find(r => r.slug === el.dataset.slug);
        if (row) renderSeriesDetail(row, gh);
      });
    });
  } catch (e) {
    listEl.innerHTML = `<p style="color:var(--danger);font-size:13px">${escHtml(e.message)}</p>`;
  }
}

function renderSeriesDetail(row, gh) {
  const detailEl = document.getElementById('mseries-detail');
  const listEl = document.getElementById('mseries-list');
  if (!detailEl || !listEl) return;
  listEl.style.display = 'none';
  detailEl.style.display = '';
  detailEl.innerHTML = `
    <button id="mback" style="background:none;border:none;color:var(--accent);font-size:13px;cursor:pointer;padding:0 0 12px 0">← All series</button>
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:12px">
      <h3 style="font-size:16px">${escHtml(row.frontmatter.title ?? row.slug)}</h3>
      <button class="btn-primary" id="mdetail-genreq" style="font-size:12px;padding:6px 12px">Generate Requirements</button>
    </div>
    <div id="mdetail-req-status"></div>
    ${row.occurrences.map(occ => `
      <div class="result-item" style="margin-bottom:8px">
        <div style="font-weight:600;margin-bottom:6px">${occ.date}</div>
        ${occ.notes ? `<p style="font-size:12px;color:var(--text-2);margin-bottom:4px"><strong>Notes:</strong> ${escHtml(occ.notes)}</p>` : ''}
        ${occ.decisions ? `<p style="font-size:12px;color:var(--text-2);margin-bottom:4px"><strong>Decisions:</strong> ${escHtml(occ.decisions)}</p>` : ''}
        ${occ.actions ? `<p style="font-size:12px;color:var(--text-2)"><strong>Actions:</strong> ${escHtml(occ.actions)}</p>` : ''}
      </div>
    `).join('')}
  `;
  document.getElementById('mback').addEventListener('click', () => {
    detailEl.style.display = 'none';
    listEl.style.display = '';
  });
  document.getElementById('mdetail-genreq').addEventListener('click', () => {
    const statusEl = document.getElementById('mdetail-req-status');
    if (statusEl) statusEl.innerHTML =
      '<p style="font-size:12px;color:var(--muted)">Open the meeting drawer (calendar button) and use the Requirements tab to generate tickets for this series.</p>';
  });
}

function escHtml(s) {
  return String(s ?? '').replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}
