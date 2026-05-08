export function formatProject({ name, status, goal, milestones, notes }) {
  const lines = [
    `### Project: ${name}`,
    `**Status:** ${status}`,
    `**Goal:** ${goal}`,
    '',
  ];
  if (milestones.trim()) {
    lines.push('**Milestones:**');
    milestones.trim().split('\n').forEach(m => {
      const t = m.trim();
      lines.push(t.startsWith('[x]') || t.startsWith('[X]')
        ? `- [x] ${t.replace(/^\[x\]\s*/i, '')}`
        : `- [ ] ${t.replace(/^\[\s*\]\s*/, '')}`);
    });
    lines.push('');
  }
  if (notes.trim()) {
    lines.push('**Notes:**');
    lines.push(notes.trim());
    lines.push('');
  }
  lines.push('---');
  return lines.join('\n') + '\n';
}

export function renderProjects(container, config, gh) {
  container.innerHTML = `
    <div class="card">
      <div class="fields-row">
        <div>
          <p class="section-label">Project name</p>
          <input id="pname" class="search-input" type="text" placeholder="Redesign Auth Flow" style="width:100%;box-sizing:border-box">
        </div>
        <div>
          <p class="section-label">Status</p>
          <select id="pstatus" class="search-input" style="width:100%;box-sizing:border-box">
            <option>Active</option>
            <option>On Hold</option>
            <option>Completed</option>
            <option>Cancelled</option>
          </select>
        </div>
      </div>
      <p class="section-label" style="margin-top:12px">Goal (one sentence)</p>
      <input id="pgoal" class="search-input" type="text" placeholder="What does this project achieve?" style="width:100%;box-sizing:border-box;margin-bottom:12px">
      <p class="section-label">Milestones (one per line — prefix with [x] if done)</p>
      <textarea id="pmilestones" class="textarea" placeholder="Design JWT schema&#10;[x] Audit existing code&#10;Implement token refresh"></textarea>
      <p class="section-label">Notes (optional)</p>
      <textarea id="pnotes" class="textarea" style="min-height:80px" placeholder="Background context, links, stakeholder info..."></textarea>
      <div class="divider"></div>
      <div class="button-row">
        <button id="psave" class="btn-primary">Save project</button>
      </div>
      <div id="pstatusmsg"></div>
    </div>`;

  document.getElementById('psave').onclick = async () => {
    const name = document.getElementById('pname').value.trim();
    if (!name) { document.getElementById('pstatusmsg').innerHTML = '<p style="color:var(--error)">Project name required</p>'; return; }
    const md = formatProject({
      name,
      status: document.getElementById('pstatus').value,
      goal: document.getElementById('pgoal').value.trim(),
      milestones: document.getElementById('pmilestones').value,
      notes: document.getElementById('pnotes').value,
    });
    document.getElementById('psave').disabled = true;
    document.getElementById('psave').textContent = 'Saving…';
    try {
      await gh.commitFile('PROJECTS.md', md, `feat: add project — ${name}`);
      document.getElementById('pstatusmsg').innerHTML = '<p style="color:var(--success,green)">Saved to PROJECTS.md</p>';
      ['pname','pgoal','pmilestones','pnotes'].forEach(id => { document.getElementById(id).value = ''; });
    } catch (e) {
      document.getElementById('pstatusmsg').innerHTML = `<p style="color:var(--error)">${e.message}</p>`;
    } finally {
      document.getElementById('psave').disabled = false;
      document.getElementById('psave').textContent = 'Save project';
    }
  };
}
