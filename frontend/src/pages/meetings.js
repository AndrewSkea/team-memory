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
      await gh.commitFile('MEETINGS.md', md, `feat: add meeting notes — ${title}`);
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
