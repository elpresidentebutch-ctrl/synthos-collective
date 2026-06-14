// utils/trialUtils.js – simple JSON‑file trial store & quota management
import fs from "fs";
import path from "path";

const STORE_PATH = path.resolve(process.cwd(), "server/trialStore.json");

function loadStore() {
  if (!fs.existsSync(STORE_PATH)) {
    fs.writeFileSync(STORE_PATH, JSON.stringify({ users: {} }, null, 2));
  }
  return JSON.parse(fs.readFileSync(STORE_PATH, "utf-8"));
}

function saveStore(store) {
  fs.writeFileSync(STORE_PATH, JSON.stringify(store, null, 2));
}

/** Create a new trial user – 7‑day validity, 200 calls */
export function createTrial(email) {
  const store = loadStore();
  const token = Buffer.from(`${email}:${Date.now()}`).toString("base64");
  const user = {
    email,
    token,
    createdAt: new Date().toISOString(),
    expiresAt: new Date(Date.now() + 7 * 24 * 60 * 60 * 1000).toISOString(),
    quota: 200,
    used: 0,
  };
  store.users[token] = user;
  saveStore(store);
  return { token: user.token, expiresAt: user.expiresAt, quota: user.quota };
}

/** Consume one quota unit – throws on errors */
export function consumeQuota(token) {
  const store = loadStore();
  const user = store.users[token];
  if (!user) throw new Error("Invalid trial token");
  if (new Date() > new Date(user.expiresAt)) throw new Error("Trial period expired");
  if (user.used >= user.quota) throw new Error("Trial quota exhausted");
  user.used += 1;
  saveStore(store);
  return { remaining: user.quota - user.used };
}

/** Retrieve user info – for UI status */
export function getUserInfo(token) {
  const store = loadStore();
  return store.users[token] || null;
}
