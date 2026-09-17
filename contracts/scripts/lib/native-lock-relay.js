const { ethers } = require("ethers");

// The native chain represents SYN as whole, indivisible units (decimals =
// 0). The wrapped SYN ERC20 on EVM is an ordinary 18-decimal token. Every
// amount that crosses this bridge has to pass through this scale factor
// exactly once, in the right direction -- see cmd/bridgerelayer's Go
// counterpart (wrappedSynUnitScale) for the reverse (burn -> release)
// conversion.
const WRAPPED_SYN_DECIMALS = 18n;
const WRAPPED_SYN_UNIT_SCALE = 10n ** WRAPPED_SYN_DECIMALS;

/**
 * Derives a deterministic bytes32 sourceEventId from a native bridge lock
 * event's own unique id. It has to be deterministic so that independent
 * relayer instances, watching the same native lock, each compute the same
 * value and their approveMint calls actually converge on the same
 * messageId for quorum -- a random or relayer-local id would mean no two
 * relayers could ever agree on what they're approving.
 */
function computeSourceEventId(nativeEventId) {
  if (!nativeEventId || typeof nativeEventId !== "string") {
    throw new Error("nativeEventId must be a non-empty string");
  }
  return ethers.keccak256(ethers.toUtf8Bytes(`SYNTHOS_NATIVE_LOCK_V1:${nativeEventId}`));
}

/**
 * Converts a native lock's whole-unit SYN amount into the 18-decimal
 * wrapped-SYN amount SYNTHOSSynBridgeMinter.approveMint expects.
 */
function wrappedAmountFromNativeUnits(nativeAmount) {
  const amount = BigInt(nativeAmount);
  if (amount <= 0n) {
    throw new Error("native lock amount must be positive");
  }
  return amount * WRAPPED_SYN_UNIT_SCALE;
}

function addressesEqual(a, b) {
  if (!a || !b) return false;
  return String(a).toLowerCase() === String(b).toLowerCase();
}

/**
 * Decides whether a native bridge_lock_native event is real, on-topic
 * backing for a mint on THIS specific EVM bridge -- not merely a transfer
 * that happens to carry matching metadata. In particular, this checks the
 * event's `recipient` (who the funds actually, protocol-recorded, went to
 * on the native chain) against the configured lock/escrow address, rather
 * than trusting only the sender-supplied destination_chain_id/recipient
 * metadata. Without that check, anyone could tag an ordinary transfer to
 * their own second wallet as a "lock" and have it minted on EVM with
 * nothing real backing it.
 */
function isMintableNativeLock(event, { lockAddress, evmChainId, assetId = "syn" }) {
  if (!event || event.type !== "bridge_lock_native") return false;
  if (!lockAddress || !evmChainId) {
    throw new Error("lockAddress and evmChainId are required");
  }
  const amount = Number(event.amount);
  if (!Number.isFinite(amount) || amount <= 0) return false;
  if ((event.asset_id || "syn") !== assetId) return false;
  if (!addressesEqual(event.recipient, lockAddress)) return false;
  if (String(event.destination_chain_id) !== String(evmChainId)) return false;
  if (!ethers.isAddress(event.destination_recipient)) return false;
  return true;
}

/**
 * Extracts a human-readable revert reason from an ethers v6 error, across
 * the handful of shapes a reverted require()/custom error can take.
 */
function revertReason(err) {
  if (!err) return "unknown error";
  return (
    err.reason ||
    err.shortMessage ||
    err.info?.error?.message ||
    err.error?.message ||
    err.message ||
    String(err)
  );
}

module.exports = {
  WRAPPED_SYN_UNIT_SCALE,
  computeSourceEventId,
  wrappedAmountFromNativeUnits,
  addressesEqual,
  isMintableNativeLock,
  revertReason,
};
