const STORE = "kv";

function openDB(name) {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(name, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(STORE);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

function tx(db, mode) {
  return db.transaction(STORE, mode).objectStore(STORE);
}

function promisifyReq(req) {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

export class Cache {
  constructor(dbName = "team-memory") {
    this.dbName = dbName;
    this._db = null;
  }
  async _open() {
    if (!this._db) this._db = await openDB(this.dbName);
    return this._db;
  }
  async get(key) {
    const db = await this._open();
    return promisifyReq(tx(db, "readonly").get(key));
  }
  async set(key, value) {
    const db = await this._open();
    return promisifyReq(tx(db, "readwrite").put(value, key));
  }
  async delete(key) {
    const db = await this._open();
    return promisifyReq(tx(db, "readwrite").delete(key));
  }
  async clear() {
    const db = await this._open();
    return promisifyReq(tx(db, "readwrite").clear());
  }
}
