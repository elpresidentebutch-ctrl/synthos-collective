(function (root, factory) {
  const api = factory();
  if (typeof module === "object" && module.exports) module.exports = api;
  root.SynthosRuntime = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  function detect(input = {}) {
    const ua = input.userAgent || "";
    const standalone = Boolean(input.standalone);
    const serviceWorker = Boolean(input.serviceWorker);
    const periodicSync = Boolean(input.periodicSync);
    const backgroundSync = Boolean(input.backgroundSync);
    const nativeBridge = Boolean(input.nativeBridge);
    const ios = /iPhone|iPad|iPod/i.test(ua);
    const android = /Android/i.test(ua);
    const mobile = ios || android || /Mobile/i.test(ua);

    if (nativeBridge) {
      return {
        tier: "native-validator",
        label: "Continuous Immune Validator",
        continuous: true,
        consensusEligible: true,
        detail: "Native background runtime available.",
      };
    }
    if (!mobile && standalone && serviceWorker) {
      return {
        tier: "installed-light-node",
        label: "Installed Immune Light Node",
        continuous: false,
        consensusEligible: false,
        detail: "Verifies while active; background heartbeat is best-effort.",
      };
    }
    if (android && standalone && serviceWorker && (periodicSync || backgroundSync)) {
      return {
        tier: "android-sentinel",
        label: "Android Immune Sentinel",
        continuous: false,
        consensusEligible: false,
        detail: "Syncs while active and wakes opportunistically under Android policy.",
      };
    }
    if (ios) {
      return {
        tier: "ios-sentinel",
        label: "iPhone/iPad Immune Sentinel",
        continuous: false,
        consensusEligible: false,
        detail: "Syncs when opened; iOS may suspend it in the background.",
      };
    }
    return {
      tier: "browser-sentinel",
      label: "Browser Immune Sentinel",
      continuous: false,
      consensusEligible: false,
      detail: "Verifies while active and catches up when reopened.",
    };
  }

  function fromBrowser() {
    const standalone = typeof window !== "undefined"
      && window.matchMedia?.("(display-mode: standalone)").matches;
    return detect({
      userAgent: typeof navigator !== "undefined" ? navigator.userAgent : "",
      standalone: Boolean(standalone || (typeof navigator !== "undefined" && navigator.standalone)),
      serviceWorker: typeof navigator !== "undefined" && "serviceWorker" in navigator,
      periodicSync: typeof ServiceWorkerRegistration !== "undefined"
        && "periodicSync" in ServiceWorkerRegistration.prototype,
      backgroundSync: typeof ServiceWorkerRegistration !== "undefined"
        && "sync" in ServiceWorkerRegistration.prototype,
      nativeBridge: typeof window !== "undefined" && Boolean(window.__SYNTHOS_NATIVE__),
    });
  }

  return { detect, fromBrowser };
});
