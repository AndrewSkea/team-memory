import { strict as assert } from 'node:assert';
import { describe, it } from 'node:test';
import { parseMsgFile, _decodeUTF16, _parseFiletime } from '../../frontend/src/services/msgparser.js';

describe('_decodeUTF16', () => {
  it('decodes ASCII as UTF-16 LE bytes', () => {
    // "Hi" encoded as UTF-16 LE: 48 00 69 00
    const b = new Uint8Array([0x48, 0x00, 0x69, 0x00]);
    assert.equal(_decodeUTF16(b), 'Hi');
  });
  it('stops at null terminator', () => {
    const b = new Uint8Array([0x48, 0x00, 0x00, 0x00, 0x69, 0x00]);
    assert.equal(_decodeUTF16(b), 'H');
  });
});

describe('_parseFiletime', () => {
  it('converts Windows FILETIME to Date', () => {
    // Unix epoch in FILETIME = 116444736000000000 (100ns intervals since 1601-01-01)
    // = 0x019DB1DED53E8000
    // lo = 0xD53E8000, hi = 0x019DB1DE
    const lo = 0xD53E8000;
    const hi = 0x019DB1DE;
    const b = new Uint8Array(8);
    const dv = new DataView(b.buffer);
    dv.setUint32(0, lo, true);
    dv.setUint32(4, hi, true);
    const d = _parseFiletime(b);
    assert.equal(d.getFullYear(), 1970);
    assert.equal(d.getMonth(), 0);
    assert.equal(d.getDate(), 1);
  });
});

describe('parseMsgFile', () => {
  it('throws on invalid magic bytes', () => {
    const buf = new ArrayBuffer(512);
    assert.throws(() => parseMsgFile(buf), /Not a valid .msg file/);
  });
});
