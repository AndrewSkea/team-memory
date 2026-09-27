import { api } from "../services/api.js";
import { parseMsgFile } from "../services/msgparser.js";
import { MEETING_TEMPLATES, applyMeetingTemplate, formatMeeting } from "../lib/formats.js";
import { splitList } from "../lib/view.js";

export function renderMeetings(root, { toast }) {
  const today = new Date().toISOString().slice(0, 10);
  root.innerHTML = `
    <div class="card">
      <label class="drop-zone" id="mdrop">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="20" height="20"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
        Drop an Outlook invite (.msg) or click to browse
        <input type="file" accept=".msg" id="mfile" hidden>
      </label>
      <div class="template-row">
        <label for="mtmpl">Template</label>
        <select id="mtmpl">${MEETING_TEMPLATES.map(t => `<option value="${t.name}">${t.name}</option>`).join("")}</select>
      </div>
      <div class="stack">
        <input id="mtitle" class="search-input" type="text" placeholder="Meeting title">
        <div class="fields-row">
          <input id="mdate" class="search-input" type="date" value="${today}" aria-label="Date">
          <input id="mattendees" class="search-input" type="text" placeholder="Attendees">
        </div>
        <textarea id="mdecisions" placeholder="Decisions — one per line"></textarea>
        <textarea id="mactions" placeholder="Action items — one per line (Owner: task)"></textarea>
        <input id="mabout" class="search-input" type="text" placeholder="About which systems? (optional, comma-separated)">
      </div>
      <div class="divider"></div>
      <div class="button-row"><button id="msave" class="btn-primary">Save meeting notes</button></div>
    </div>`;
  const $ = sel => root.querySelector(sel);

  $("#mtmpl").addEventListener("change", e => {
    const f = applyMeetingTemplate(e.target.value);
    if (f.title) $("#mtitle").value = f.title;
    $("#mdecisions").value = f.decisions;
    $("#mactions").value = f.actions;
  });

  async function handleMsgFile(file) {
    if (!file || !file.name.toLowerCase().endsWith(".msg")) return;
    try {
      const { subject, date, displayTo, displayCc, body } = parseMsgFile(await file.arrayBuffer());
      if (subject) $("#mtitle").value = subject;
      if (date) $("#mdate").value = date.toISOString().slice(0, 10);
      const attendees = [displayTo, displayCc].filter(Boolean).join(", ");
      if (attendees) $("#mattendees").value = attendees;
      if (body) $("#mdecisions").value = body.trim().slice(0, 4000);
      toast("Filled in from the invite — review before saving.");
    } catch {
      toast("Could not read that .msg file", true);
    }
  }
  const drop = $("#mdrop");
  drop.addEventListener("dragover", e => { e.preventDefault(); drop.classList.add("drag-over"); });
  drop.addEventListener("dragleave", () => drop.classList.remove("drag-over"));
  drop.addEventListener("drop", e => { e.preventDefault(); drop.classList.remove("drag-over"); handleMsgFile(e.dataTransfer.files[0]); });
  $("#mfile").addEventListener("change", e => handleMsgFile(e.target.files[0]));

  $("#msave").onclick = async () => {
    const title = $("#mtitle").value.trim();
    if (!title) { toast("Give the meeting a title.", true); return; }
    const text = formatMeeting({ title, date: $("#mdate").value, attendees: $("#mattendees").value,
      decisions: $("#mdecisions").value, actionItems: $("#mactions").value });
    $("#msave").disabled = true;
    try {
      await api.remember({ title: `Meeting: ${title}`, text, type: "meeting", about: splitList($("#mabout").value), classify: true, source: "ui" });
      toast("Meeting notes saved.");
      for (const id of ["#mtitle", "#mattendees", "#mdecisions", "#mactions", "#mabout"]) $(id).value = "";
    } catch (e) {
      toast("Save failed: " + e.message, true);
    } finally {
      $("#msave").disabled = false;
    }
  };
}
