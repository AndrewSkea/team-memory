const HEADER = "# INDEX for team-memory";

export function parseIndex(text) {
  const entries = [];
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    const parts = line.split("|").map(p => p.trim());
    if (parts.length < 3) continue;
    entries.push({ path: parts[0], scope: parts[1], topics: parts.slice(2).join(" | ") });
  }
  return { entries };
}

export function serializeIndex(index) {
  const lines = [HEADER];
  for (const e of index.entries) {
    lines.push(`${e.path} | ${e.scope} | ${e.topics}`);
  }
  return lines.join("\n") + "\n";
}

export function upsertEntry(index, entry) {
  const i = index.entries.findIndex(e => e.path === entry.path);
  if (i >= 0) index.entries[i] = entry;
  else index.entries.push(entry);
  return index;
}
