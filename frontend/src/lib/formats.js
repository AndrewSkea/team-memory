// Pure Markdown formatters for structured memory bodies.

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

export function formatAction({ text, priority, owner, due }) {
  let line = `- [ ] ${priority} | ${text}`;
  if (owner.trim()) line += ` | Owner: ${owner.trim()}`;
  if (due) line += ` | Due: ${due}`;
  return line + '\n';
}

export function toggleActionLine(content, line) {
  if (line.startsWith('- [ ]')) {
    return content.replace(line, line.replace('- [ ]', '- [x]'));
  }
  return content.replace(line, line.replace('- [x]', '- [ ]'));
}

export function formatReminder({ title, dueDate, details }) {
  const lines = [`**Due:** ${dueDate}`];
  if (details && details.trim()) lines.push('', details.trim());
  return lines.join('\n') + '\n';
}

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
