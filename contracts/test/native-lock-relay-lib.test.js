const { expect } = require("chai");
const { ethers } = require("hardhat");
const {
  WRAPPED_SYN_UNIT_SCALE,
  computeSourceEventId,
  wrappedAmountFromNativeUnits,
  addressesEqual,
  isMintableNativeLock,
  revertReason,
} = require("../scripts/lib/native-lock-relay");

const LOCK_ADDRESS = "0x000000000000000000000000000000000000b07d";
const EVM_CHAIN_ID = "84532";
const EVM_RECIPIENT = "0x1111111111111111111111111111111111111111";

function baseEvent(overrides = {}) {
  return {
    id: "native-lock-1",
    type: "bridge_lock_native",
    amount: 500,
    asset_id: "syn",
    recipient: LOCK_ADDRESS,
    destination_chain_id: EVM_CHAIN_ID,
    destination_recipient: EVM_RECIPIENT,
    ...overrides,
  };
}

describe("native-lock-relay lib", function () {
  describe("computeSourceEventId", function () {
    it("is deterministic for the same native event id", function () {
      const a = computeSourceEventId("native-lock-1");
      const b = computeSourceEventId("native-lock-1");
      expect(a).to.equal(b);
      expect(a).to.match(/^0x[0-9a-f]{64}$/);
    });

    it("differs across different native event ids", function () {
      expect(computeSourceEventId("native-lock-1")).to.not.equal(computeSourceEventId("native-lock-2"));
    });

    it("rejects an empty id", function () {
      expect(() => computeSourceEventId("")).to.throw();
    });
  });

  describe("wrappedAmountFromNativeUnits", function () {
    it("scales whole native SYN into 18-decimal wrapped units", function () {
      expect(wrappedAmountFromNativeUnits(500)).to.equal(500n * WRAPPED_SYN_UNIT_SCALE);
    });

    it("handles amounts larger than Number.MAX_SAFE_INTEGER via BigInt-safe string input", function () {
      expect(wrappedAmountFromNativeUnits("100000000000")).to.equal(
        100_000_000_000n * WRAPPED_SYN_UNIT_SCALE
      );
    });

    it("rejects zero or negative amounts", function () {
      expect(() => wrappedAmountFromNativeUnits(0)).to.throw();
      expect(() => wrappedAmountFromNativeUnits(-5)).to.throw();
    });
  });

  describe("addressesEqual", function () {
    it("compares case-insensitively", function () {
      expect(addressesEqual(LOCK_ADDRESS, LOCK_ADDRESS.toUpperCase())).to.equal(true);
    });
    it("is false for missing values", function () {
      expect(addressesEqual(LOCK_ADDRESS, undefined)).to.equal(false);
      expect(addressesEqual(undefined, LOCK_ADDRESS)).to.equal(false);
    });
  });

  describe("isMintableNativeLock", function () {
    const opts = { lockAddress: LOCK_ADDRESS, evmChainId: EVM_CHAIN_ID };

    it("accepts a well-formed, on-topic lock", function () {
      expect(isMintableNativeLock(baseEvent(), opts)).to.equal(true);
    });

    it("rejects a different event type", function () {
      expect(isMintableNativeLock(baseEvent({ type: "bridge_release_native" }), opts)).to.equal(false);
    });

    it("rejects when the funds did not land at the configured lock address -- the core security check", function () {
      // This is exactly the case a self-tagged fake lock would try: real
      // metadata, wrong (or no) actual recipient.
      expect(
        isMintableNativeLock(baseEvent({ recipient: "0x000000000000000000000000000000000000dead" }), opts)
      ).to.equal(false);
    });

    it("rejects a destination_chain_id that doesn't match this EVM chain", function () {
      expect(isMintableNativeLock(baseEvent({ destination_chain_id: "1" }), opts)).to.equal(false);
    });

    it("rejects a non-address destination_recipient", function () {
      expect(isMintableNativeLock(baseEvent({ destination_recipient: "not-an-address" }), opts)).to.equal(
        false
      );
    });

    it("rejects zero or missing amount", function () {
      expect(isMintableNativeLock(baseEvent({ amount: 0 }), opts)).to.equal(false);
      expect(isMintableNativeLock(baseEvent({ amount: undefined }), opts)).to.equal(false);
    });

    it("rejects an asset other than syn", function () {
      expect(isMintableNativeLock(baseEvent({ asset_id: "wbtc" }), opts)).to.equal(false);
    });

    it("throws if not given a lockAddress/evmChainId to check against", function () {
      expect(() => isMintableNativeLock(baseEvent(), {})).to.throw();
    });
  });

  describe("revertReason", function () {
    it("prefers a decoded .reason", function () {
      expect(revertReason({ reason: "already processed", message: "execution reverted" })).to.equal(
        "already processed"
      );
    });
    it("falls back to .shortMessage, then .message", function () {
      expect(revertReason({ shortMessage: "already approved" })).to.equal("already approved");
      expect(revertReason({ message: "network error" })).to.equal("network error");
    });
    it("handles a bare string/undefined without throwing", function () {
      expect(revertReason(undefined)).to.be.a("string");
    });
  });
});
