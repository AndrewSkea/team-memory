export function slugify(name) {
  return name.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
}

export function formatTopic({ name, date, content }) {
  return `# ${name} — Knowledge Dump\n\n*Last updated: ${date}*\n\n${content.trim()}\n\n---\n`;
}

export function renderTopics(container, config, gh) {
  const today = new Date().toISOString().slice(0, 10);
  container.innerHTML = `
    <div class="card">
      <p class="section-label">Topic name</p>
      <input id="tname" class="search-input" type="text" placeholder="React Query, JWT Auth, Go Channels…" style="width:100%;box-sizing:border-box;margin-bottom:4px">
      <p class="muted" style="font-size:12px;margin:0 0 12px">Saved to <code>TOPICS/&lt;slug&gt;.md</code> — existing topics updated in place.</p>
      <p class="section-label">Content</p>
      <textarea id="tcontent" class="textarea" style="min-height:200px" placeholder="Dump everything you know about this topic…"></textarea>
      <div class="divider"></div>
      <div class="button-row">
        <button id="tsave" class="btn-primary">Save topic</button>
      </div>
      <div id="tstatus"></div>
    </div>`;

  document.getElementById('tsave').onclick = async () => {
    const name = document.getElementById('tname').value.trim();
    const content = document.getElementById('tcontent').value.trim();
    if (!name) { document.getElementById('tstatus').innerHTML = '<p style="color:var(--error)">Topic name required</p>'; return; }
    if (!content) { document.getElementById('tstatus').innerHTML = '<p style="color:var(--error)">Content required</p>'; return; }
    const slug = slugify(name);
    const path = `TOPICS/${slug}.md`;
    const md = formatTopic({ name, date: today, content });
    document.getElementById('tsave').disabled = true;
    document.getElementById('tsave').textContent = 'Saving…';
    try {
      const f = await gh.getFile(path);
      if (f.exists) {
        await gh.putContent(path, md, `feat: update topic — ${name}`);
      } else {
        await gh.putContent(path, md, `feat: add topic — ${name}`);
      }
      document.getElementById('tstatus').innerHTML = `<p style="color:var(--success,green)">Saved to ${path}</p>`;
    } catch (e) {
      document.getElementById('tstatus').innerHTML = `<p style="color:var(--error)">${e.message}</p>`;
    } finally {
      document.getElementById('tsave').disabled = false;
      document.getElementById('tsave').textContent = 'Save topic';
    }
  };
}
