export function renderEntry(e) {
  const bullets = (e.bullets ?? []).map(b => `- ${b}`).join(" ; ");
  return [
    `### Entry: ${e.timestamp} — ${e.shortTitle}`,
    `**Scope:** ${e.scope}`,
    `**Type:** ${e.type}`,
    `**Tags:** ${e.tags ?? ""}`,
    `**Source:** ${e.source}`,
    `**Summary:** ${e.summary ?? ""}`,
    `**Bullets:** ${bullets}`,
    `**Full:**`,
    e.full ?? "",
    "",
  ].join("\n");
}

export function appendEntry(existing, entry) {
  const block = renderEntry(entry);
  if (!existing || !existing.trim()) return block;
  const sep = existing.endsWith("\n") ? "\n" : "\n\n";
  return existing + sep + block;
}
