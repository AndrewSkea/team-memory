import { parseMsgFile } from '../services/msgparser.js';

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
  const today = new Date().toISOString().slice(0, 10);
  container.innerHTML = `
    <div class="card">
      <div class="drop-zone" id="mdrop">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="20" height="20" style="display:block;margin:0 auto 6px"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
        Drop an Outlook invite (.msg) or click to browse
        <input type="file" accept=".msg" id="mfile">
      </div>
      <div class="template-row">
        <label for="mtmpl">Template</label>
        <select id="mtmpl">
          ${MEETING_TEMPLATES.map(t => `<option value="${t.name}">${t.name}</option>`).join('')}
        </select>
      </div>
      <p class="section-label">Meeting title</p>
      <input id="mtitle" class="search-input" type="text" placeholder="Sprint Planning" style="width:100%;box-sizing:border-box;margin-bottom:12px">
      <div class="fields-row">
        <div>
          <p class="section-label">Date</p>
          <input id="mdate" class="search-input" type="date" value="${today}" style="width:100%;box-sizing:border-box">
        </div>
        <div>
          <p class="section-label">Attendees</p>
          <input id="mattendees" class="search-input" type="text" placeholder="Alice, Bob, Charlie" style="width:100%;box-sizing:border-box">
        </div>
      </div>
      <p class="section-label" style="margin-top:12px">Decisions (one per line)</p>
      <textarea id="mdecisions" class="textarea" placeholder="We decided to...&#10;We agreed on..."></textarea>
      <p class="section-label">Action items (one per line, format: Owner: task)</p>
      <textarea id="mactions" class="textarea" placeholder="Alice: set up project board&#10;Bob: draft sprint template"></textarea>
      <div class="divider"></div>
      <div class="button-row">
        <button id="msave" class="btn-primary">Save meeting notes</button>
      </div>
      <div id="mstatus"></div>
    </div>`;

  document.getElementById('mtmpl').addEventListener('change', e => {
    const f = applyMeetingTemplate(e.target.value);
    if (f.title) document.getElementById('mtitle').value = f.title;
    document.getElementById('mdecisions').value = f.decisions;
    document.getElementById('mactions').value   = f.actions;
  });

  function handleMsgFile(file) {
    if (!file || !file.name.endsWith('.msg')) return;
    file.arrayBuffer().then(buf => {
      try {
        const { subject, date, displayTo, displayCc, body } = parseMsgFile(buf);
        if (subject) document.getElementById('mtitle').value = subject;
        if (date) document.getElementById('mdate').value = date.toISOString().slice(0, 10);
        const attendees = [displayTo, displayCc].filter(Boolean).join(', ');
        if (attendees) document.getElementById('mattendees').value = attendees;
        if (body) document.getElementById('mdecisions').value = body.trim();
        document.getElementById('mstatus').innerHTML = '<p style="color:var(--green)">Populated from .msg file — review and edit before saving</p>';
      } catch {
        document.getElementById('mstatus').innerHTML = '<p style="color:var(--danger)">Could not parse .msg file</p>';
      }
    });
  }

  const drop = document.getElementById('mdrop');
  drop.addEventListener('dragover', e => { e.preventDefault(); drop.classList.add('drag-over'); });
  drop.addEventListener('dragleave', () => drop.classList.remove('drag-over'));
  drop.addEventListener('drop', e => {
    e.preventDefault();
    drop.classList.remove('drag-over');
    handleMsgFile(e.dataTransfer.files[0]);
  });
  document.getElementById('mfile').addEventListener('change', e => handleMsgFile(e.target.files[0]));

  document.getElementById('msave').onclick = async () => {
    const title = document.getElementById('mtitle').value.trim();
    if (!title) { document.getElementById('mstatus').innerHTML = '<p style="color:var(--error)">Title required</p>'; return; }
    const md = formatMeeting({
      title,
      date: document.getElementById('mdate').value,
      attendees: document.getElementById('mattendees').value,
      decisions: document.getElementById('mdecisions').value,
      actionItems: document.getElementById('mactions').value,
    });
    document.getElementById('msave').disabled = true;
    document.getElementById('msave').textContent = 'Saving…';
    try {
      await gh.commitFile({ path: 'MEETINGS.md', append: md, message: `feat: add meeting notes — ${title}` });
      document.getElementById('mstatus').innerHTML = '<p style="color:var(--success,green)">Saved to MEETINGS.md</p>';
      ['mtitle','mattendees','mdecisions','mactions'].forEach(id => { document.getElementById(id).value = ''; });
    } catch (e) {
      document.getElementById('mstatus').innerHTML = `<p style="color:var(--error)">${e.message}</p>`;
    } finally {
      document.getElementById('msave').disabled = false;
      document.getElementById('msave').textContent = 'Save meeting notes';
    }
  };
}
