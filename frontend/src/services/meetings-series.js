export function deriveSlug(subject) {
  let s = subject.trim();
  // Strip RE:/FW:/FWD: prefixes (case-insensitive, possibly multiple)
  s = s.replace(/^(RE|FW|FWD):\s*/i, '').trim();
  // Strip trailing day-date suffix like "- Mon 24 May 2026"
  s = s.replace(/\s*-\s*(Mon|Tue|Wed|Thu|Fri|Sat|Sun)\s+\d{1,2}\s+\w+\s+\d{4}\s*$/i, '').trim();
  // Strip trailing "(N)" occurrences
  s = s.replace(/\s*\(\d+\)\s*$/, '').trim();
  // Lowercase and replace whitespace with hyphens, remove non-alphanumeric (except hyphens)
  return s.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '');
}

export function parseSeries(content) {
  // Split on the frontmatter delimiters
  const parts = content.split(/^---\s*$/m);
  // parts[0] = '' (before first ---), parts[1] = frontmatter, parts[2+] = body
  const frontmatterRaw = parts[1] || '';
  const body = parts.slice(2).join('---');

  // Parse frontmatter lines
  const frontmatter = {};
  for (const line of frontmatterRaw.split('\n')) {
    const m = line.match(/^(\w+):\s*(.*)/);
    if (m) frontmatter[m[1]] = m[2].trim();
  }

  // Parse occurrence sections (## YYYY-MM-DD)
  const occurrences = [];
  const sections = body.split(/^(?=## \d{4}-\d{2}-\d{2})/m).filter(s => s.trim());

  for (const section of sections) {
    const dateMatch = section.match(/^## (\d{4}-\d{2}-\d{2})/);
    if (!dateMatch) continue;
    const date = dateMatch[1];

    const notes = extractField(section, 'Notes');
    const decisions = extractField(section, 'Decisions');
    const actions = extractField(section, 'Action Items');
    const enrichment = extractField(section, 'AI Enrichment');

    occurrences.push({ date, notes, decisions, actions, enrichment });
  }

  // Sort newest-first
  occurrences.sort((a, b) => b.date.localeCompare(a.date));

  return { frontmatter, occurrences };
}

function extractField(section, fieldName) {
  // Match **FieldName:** followed by value until next **AnyField:** or end
  const escaped = fieldName.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const re = new RegExp(`\\*\\*${escaped}:\\*\\*([\\s\\S]*?)(?=\\n\\*\\*[^*]|$)`);
  const m = section.match(re);
  if (!m) return '';
  return m[1];
}

export function buildSeriesFile({ title, slug, attendees, template }) {
  const today = new Date().toISOString().slice(0, 10);
  return `---\ntitle: ${title}\nslug: ${slug}\nattendees: ${attendees}\ntemplate: ${template}\ncreated: ${today}\n---\n`;
}

export function formatOccurrence({ date, notes, decisions, actions, enrichment }) {
  return `\n## ${date}\n**Notes:** ${notes}\n**Decisions:** ${decisions}\n**Action Items:** ${actions}\n**AI Enrichment:** ${enrichment}\n`;
}

export function appendOccurrence(content, occurrence) {
  return content + formatOccurrence(occurrence);
}
