export function formatStandup({ date, yesterday, today, blockers }) {
  const lines = [`### Standup: ${date}`, ''];
  if (yesterday.trim()) {
    lines.push('**Yesterday:**');
    yesterday.trim().split('\n').forEach(l => lines.push(`- ${l.trim()}`));
    lines.push('');
  }
  if (today.trim()) {
    lines.push('**Today:**');
    today.trim().split('\n').forEach(l => lines.push(`- ${l.trim()}`));
    lines.push('');
  }
  if (blockers.trim()) {
    lines.push('**Blockers:**');
    blockers.trim().split('\n').forEach(l => lines.push(`- ${l.trim()}`));
    lines.push('');
  }
  lines.push('---');
  return lines.join('\n') + '\n';
}

export function renderStandup(container, config, gh) {
  const today = new Date().toISOString().slice(0, 10);
  container.innerHTML = `
    <div class="card">
      <div style="display:flex;align-items:center;gap:12px;margin-bottom:12px">
        <p class="section-label" style="margin:0">Date</p>
        <input id="sdate" class="search-input" type="date" value="${today}">
      </div>
      <p class="section-label">What did I do yesterday? (one per line)</p>
      <textarea id="syesterday" class="textarea" placeholder="Fixed the auth bug&#10;Reviewed 2 PRs"></textarea>
      <p class="section-label">What will I do today? (one per line)</p>
      <textarea id="stoday" class="textarea" placeholder="Start feature X&#10;Team sync at 2pm"></textarea>
      <p class="section-label">Blockers (one per line, leave blank if none)</p>
      <textarea id="sblockers" class="textarea" style="min-height:80px" placeholder="Waiting on design review"></textarea>
      <div class="divider"></div>
      <div class="button-row">
        <button id="ssave" class="btn-primary">Save standup</button>
      </div>
      <div id="sstatus"></div>
    </div>`;

  document.getElementById('ssave').onclick = async () => {
    const md = formatStandup({
      date: document.getElementById('sdate').value,
      yesterday: document.getElementById('syesterday').value,
      today: document.getElementById('stoday').value,
      blockers: document.getElementById('sblockers').value,
    });
    document.getElementById('ssave').disabled = true;
    document.getElementById('ssave').textContent = 'Saving…';
    try {
      await gh.commitFile('STANDUPS.md', md, `feat: standup ${document.getElementById('sdate').value}`);
      document.getElementById('sstatus').innerHTML = '<p style="color:var(--success,green)">Saved to STANDUPS.md</p>';
      ['syesterday','stoday','sblockers'].forEach(id => { document.getElementById(id).value = ''; });
    } catch (e) {
      document.getElementById('sstatus').innerHTML = `<p style="color:var(--error)">${e.message}</p>`;
    } finally {
      document.getElementById('ssave').disabled = false;
      document.getElementById('ssave').textContent = 'Save standup';
    }
  };
}
