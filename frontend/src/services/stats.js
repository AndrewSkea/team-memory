const LOOKUPS_KEY = "team-memory:lookups";

export function incrementLookups() {
  const n = parseInt(localStorage.getItem(LOOKUPS_KEY) ?? "0", 10);
  localStorage.setItem(LOOKUPS_KEY, String(n + 1));
}

export function getLookups() {
  return parseInt(localStorage.getItem(LOOKUPS_KEY) ?? "0", 10);
}

// Parse all entries from a file's markdown content.
// Returns array of { source, timestamp } objects.
function parseEntries(content) {
  const entries = [];
  const headerRe = /^### Entry: ([^\s]+)/m;
  const sourceRe = /^\*\*Source:\*\* (.+)$/m;
  const blocks = content.split(/(?=^### Entry:)/m);
  for (const block of blocks) {
    const hm = block.match(headerRe);
    if (!hm) continue;
    const sm = block.match(sourceRe);
    entries.push({
      timestamp: hm[1],
      source: sm ? sm[1].trim() : "Unknown",
    });
  }
  return entries;
}

function isThisWeek(isoTimestamp) {
  try {
    const d = new Date(isoTimestamp);
    const now = new Date();
    const msAgo = now - d;
    return msAgo >= 0 && msAgo < 7 * 24 * 60 * 60 * 1000;
  } catch { return false; }
}

export async function computeStats(gh, cache) {
  // Fetch INDEX
  let idxFile;
  const cached = await cache.get("file:INDEX.md");
  if (cached) {
    idxFile = cached;
  } else {
    idxFile = await gh.getFile("INDEX.md");
    await cache.set("file:INDEX.md", idxFile);
  }

  const lines = (idxFile.content ?? "").split(/\r?\n/).filter(l => l.trim() && !l.startsWith("#"));
  const paths = lines.map(l => l.split("|")[0].trim()).filter(Boolean);

  let total = 0;
  let thisWeek = 0;
  const bySource = { Stop: 0, PreCompact: 0, UI: 0, Unknown: 0 };

  await Promise.all(paths.map(async (path) => {
    try {
      let f = await cache.get("file:" + path);
      if (!f) {
        f = await gh.getFile(path);
        if (f.exists) await cache.set("file:" + path, f);
      }
      if (!f?.exists) return;
      const entries = parseEntries(f.content ?? "");
      total += entries.length;
      for (const e of entries) {
        if (isThisWeek(e.timestamp)) thisWeek++;
        const key = bySource.hasOwnProperty(e.source) ? e.source : "Unknown";
        bySource[key]++;
      }
    } catch { /* skip unreadable files */ }
  }));

  return {
    total,
    thisWeek,
    bySource,
    filesCount: paths.length,
    lookups: getLookups(),
  };
}
