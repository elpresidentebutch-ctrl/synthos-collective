const assert = require("node:assert/strict");
const { detect } = require("../workers/mobile-validator/runtime.js");

assert.equal(detect({
  userAgent: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0)",
  standalone: true,
  serviceWorker: true,
}).tier, "ios-sentinel");

assert.equal(detect({
  userAgent: "Mozilla/5.0 (Linux; Android 15; Mobile)",
  standalone: true,
  serviceWorker: true,
  periodicSync: true,
}).tier, "android-sentinel");

assert.equal(detect({
  userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64)",
  standalone: true,
  serviceWorker: true,
}).tier, "installed-light-node");

assert.equal(detect({
  userAgent: "SYNTHOS Native Android",
  nativeBridge: true,
}).tier, "native-validator");

console.log("runtime tier tests passed");
