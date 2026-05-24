const ENDOFCHAIN = 0xFFFFFFFE;
const NOSTREAM   = 0xFFFFFFFF;

const PROP = {
  SUBJECT:       0x0037,
  DISPLAY_TO:    0x0E04,
  DISPLAY_CC:    0x0E03,
  BODY:          0x1000,
  START_DATE:    0x820D,
  DELIVERY_TIME: 0x0039,
};
const TYPE_UNICODE  = 0x001F;
const TYPE_FILETIME = 0x0040;

export function _decodeUTF16(bytes) {
  const dv = new DataView(bytes.buffer, bytes.byteOffset);
  let s = '';
  for (let i = 0; i + 1 < bytes.length; i += 2) {
    const c = dv.getUint16(i, true);
    if (c === 0) break;
    s += String.fromCharCode(c);
  }
  return s;
}

export function _parseFiletime(bytes) {
  const dv = new DataView(bytes.buffer, bytes.byteOffset);
  const lo = dv.getUint32(0, true);
  const hi = dv.getUint32(4, true);
  const ms = (hi * 4294967296 + lo) / 10000 - 11644473600000;
  return new Date(ms);
}

export function parseMsgFile(arrayBuffer) {
  const b  = new Uint8Array(arrayBuffer);
  const dv = new DataView(arrayBuffer);

  if (b[0] !== 0xD0 || b[1] !== 0xCF || b[2] !== 0x11 || b[3] !== 0xE0) {
    throw new Error('Not a valid .msg file (bad signature)');
  }

  const sectorSize   = 1 << dv.getUint16(30, true);
  const miniCutoff   = dv.getUint32(56, true);
  const fatSecCount  = dv.getUint32(44, true);
  const firstDirSec  = dv.getUint32(48, true);
  const firstMiniFAT = dv.getUint32(60, true);

  const secOff = s => 512 + s * sectorSize;

  // Build FAT from DIFAT array in header (up to 109 entries at offset 76)
  const fat = [];
  for (let i = 0; i < Math.min(109, fatSecCount); i++) {
    const s = dv.getUint32(76 + i * 4, true);
    if (s === NOSTREAM || s === ENDOFCHAIN) break;
    const off = secOff(s);
    for (let j = 0; j < sectorSize; j += 4) {
      fat.push(dv.getUint32(off + j, true));
    }
  }

  function readChain(start) {
    const parts = [];
    let sec = start;
    while (sec !== ENDOFCHAIN && sec !== NOSTREAM && sec < fat.length) {
      const off = secOff(sec);
      parts.push(b.slice(off, off + sectorSize));
      sec = fat[sec];
    }
    const out = new Uint8Array(parts.reduce((a, c) => a + c.length, 0));
    let pos = 0;
    for (const p of parts) { out.set(p, pos); pos += p.length; }
    return out;
  }

  const dirBytes = readChain(firstDirSec);
  const dirDv    = new DataView(dirBytes.buffer, dirBytes.byteOffset);

  function readEntry(i) {
    const base    = i * 128;
    const nameLen = dirDv.getUint16(base + 64, true);
    let name = '';
    for (let k = 0; k < Math.min(nameLen - 2, 62); k += 2) {
      const c = dirDv.getUint16(base + k, true);
      if (c) name += String.fromCharCode(c);
    }
    return {
      name,
      type:  dirBytes[base + 66],
      start: dirDv.getUint32(base + 116, true),
      size:  dirDv.getUint32(base + 120, true),
    };
  }

  const root       = readEntry(0);
  const miniStream = readChain(root.start);

  const miniFat = [];
  if (firstMiniFAT !== ENDOFCHAIN && firstMiniFAT !== NOSTREAM) {
    const mf   = readChain(firstMiniFAT);
    const mfDv = new DataView(mf.buffer, mf.byteOffset);
    for (let i = 0; i < mf.length; i += 4) miniFat.push(mfDv.getUint32(i, true));
  }

  function readMiniChain(start, size) {
    const parts    = [];
    let sec        = start;
    const miniSize = 64;
    while (sec !== ENDOFCHAIN && sec < miniFat.length) {
      const off = sec * miniSize;
      parts.push(miniStream.slice(off, off + miniSize));
      sec = miniFat[sec];
    }
    const out = new Uint8Array(parts.reduce((a, c) => a + c.length, 0));
    let pos = 0;
    for (const p of parts) { out.set(p, pos); pos += p.length; }
    return out.slice(0, size);
  }

  function readStream(e) {
    return e.size < miniCutoff
      ? readMiniChain(e.start, e.size)
      : readChain(e.start).slice(0, e.size);
  }

  const result = { subject: '', date: null, displayTo: '', displayCc: '', body: '' };
  const count  = dirBytes.length / 128;

  for (let i = 0; i < count; i++) {
    const e = readEntry(i);
    if (e.type !== 2) continue;
    const m = e.name.match(/^__substg1\.0_([0-9A-Fa-f]{4})([0-9A-Fa-f]{4})$/);
    if (!m) continue;
    const propId   = parseInt(m[1], 16);
    const propType = parseInt(m[2], 16);
    const data     = readStream(e);

    if (propType === TYPE_UNICODE) {
      if (propId === PROP.SUBJECT)    result.subject   = _decodeUTF16(data);
      if (propId === PROP.DISPLAY_TO) result.displayTo = _decodeUTF16(data);
      if (propId === PROP.DISPLAY_CC) result.displayCc = _decodeUTF16(data);
      if (propId === PROP.BODY)       result.body      = _decodeUTF16(data).slice(0, 2000);
    } else if (propType === TYPE_FILETIME) {
      if (propId === PROP.START_DATE)                          result.date = _parseFiletime(data);
      if (propId === PROP.DELIVERY_TIME && !result.date)       result.date = _parseFiletime(data);
    }
  }

  return result;
}
