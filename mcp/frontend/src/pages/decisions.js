export function formatDecision({ title, date, status, context, decision, consequences }) {
  const lines = [
    `### Decision: ${title}`,
    `**Date:** ${date}`,
    `**Status:** ${status}`,
    '',
  ];
  if (context.trim()) { lines.push('**Context:**', context.trim(), ''); }
  if (decision.trim()) { lines.push('**Decision:**', decision.trim(), ''); }
  if (consequences.trim()) {
    lines.push('**Consequences:**');
    consequences.trim().split('\n').forEach(c => lines.push(`- ${c.trim()}`));
    lines.push('');
  }
  lines.push('---');
  return lines.join('\n') + '\n';
}

export function renderDecisions(container, config, gh) {
  const today = new Date().toISOString().slice(0, 10);
  container.innerHTML = `
    <div class="card">
      <p class="section-label">Decision title</p>
      <input id="dtitle" class="search-input" type="text" placeholder="Use JWT for auth" style="width:100%;box-sizing:border-box;margin-bottom:12px">
      <div class="fields-row">
        <div>
          <p class="section-label">Date</p>
          <input id="ddate" class="search-input" type="date" value="${today}" style="width:100%;box-sizing:border-box">
        </div>
        <div>
          <p class="section-label">Status</p>
          <select id="dstatus" class="search-input" style="width:100%;box-sizing:border-box">
            <option>Proposed</option>
            <option>Accepted</option>
            <option>Deprecated</option>
            <option>Superseded</option>
          </select>
        </div>
      </div>
      <p class="section-label" style="margin-top:12px">Context — why is this decision needed?</p>
      <textarea id="dcontext" class="textarea" placeholder="What problem are we solving?"></textarea>
      <p class="section-label">Decision — what did we decide?</p>
      <textarea id="ddecision" class="textarea" placeholder="We will..."></textarea>
      <p class="section-label">Consequences (one per line)</p>
      <textarea id="dconsequences" class="textarea" style="min-height:80px" placeholder="Clients must handle X&#10;Y can be removed after migration"></textarea>
      <div class="divider"></div>
      <div class="button-row">
        <button id="dsave" class="btn-primary">Save decision</button>
      </div>
      <div id="dstatusmsg"></div>
    </div>`;

  document.getElementById('dsave').onclick = async () => {
    const title = document.getElementById('dtitle').value.trim();
    if (!title) { document.getElementById('dstatusmsg').innerHTML = '<p style="color:var(--error)">Title required</p>'; return; }
    const md = formatDecision({
      title,
      date: document.getElementById('ddate').value,
      status: document.getElementById('dstatus').value,
      context: document.getElementById('dcontext').value,
      decision: document.getElementById('ddecision').value,
      consequences: document.getElementById('dconsequences').value,
    });
    document.getElementById('dsave').disabled = true;
    document.getElementById('dsave').textContent = 'Saving…';
    try {
      await gh.commitFile({ path: 'DECISIONS.md', append: md, message: `feat: decision — ${title}` });
      document.getElementById('dstatusmsg').innerHTML = '<p style="color:var(--success,green)">Saved to DECISIONS.md</p>';
      ['dtitle','dcontext','ddecision','dconsequences'].forEach(id => { document.getElementById(id).value = ''; });
    } catch (e) {
      document.getElementById('dstatusmsg').innerHTML = `<p style="color:var(--error)">${e.message}</p>`;
    } finally {
      document.getElementById('dsave').disabled = false;
      document.getElementById('dsave').textContent = 'Save decision';
    }
  };
}
