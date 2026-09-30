import nacl from "npm:tweetnacl@1.0.3";

export type Json = null | boolean | number | string | Json[] | { [key: string]: Json };
export type MessageType = "proposal" | "vote";

export interface ValidatorRecord {
  id: string;
  publicKey: string; // base64 Ed25519 public key
  url: string;
}

export interface Genesis {
  protocolVersion: 1;
  chainId: string;
  network: string;
  genesisTime: string;
  heartbeatMs: 15000;
  quorum: 5;
  validators: ValidatorRecord[];
  allocations: Record<string, string>;
}

export interface Transaction {
  id: string;
  sender: string;
  nonce: number;
  payload: Json;
  receivedAt: number;
}

export interface Block {
  version: 1;
  chainId: string;
  height: number;
  round: number;
  proposer: string;
  previousHash: string;
  timestamp: number;
  transactions: Transaction[];
  hash: string;
}

export interface VotePayload {
  blockHash: string;
  decision: "accept";
}

export interface SignedMessage {
  version: 1;
  network: string;
  type: MessageType;
  validatorId: string;
  height: number;
  round: number;
  timestamp: number;
  nonce: string;
  payload: Block | VotePayload;
  signature: string;
}

export interface Head {
  height: number;
  hash: string;
  finalizedAt: number;
}

export interface Store {
  get<T>(key: string): Promise<T | null>;
  set<T>(key: string, value: T): Promise<void>;
  list<T>(prefix: string, limit?: number): Promise<Array<{ key: string; value: T }>>;
  putIfAbsent<T>(key: string, value: T): Promise<boolean>;
  advanceHead(expectedHeight: number, head: Head): Promise<boolean>;
}

export interface RuntimeContext {
  waitUntil(promise: Promise<unknown>): void;
}

export interface NodeOptions {
  validatorId: string;
  privateKey: string; // base64 Ed25519 32-byte seed
  genesis: Genesis;
  store: Store;
  fetcher?: typeof fetch;
  now?: () => number;
  logger?: Pick<Console, "log" | "warn" | "error">;
}

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

function base64ToBytes(value: string): Uint8Array {
  const binary = atob(value);
  return Uint8Array.from(binary, (char) => char.charCodeAt(0));
}

function canonicalValue(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonicalValue);
  if (value && typeof value === "object") {
    const source = value as Record<string, unknown>;
    const result: Record<string, unknown> = {};
    for (const key of Object.keys(source).sort()) {
      if (source[key] !== undefined) result[key] = canonicalValue(source[key]);
    }
    return result;
  }
  return value;
}

export function canonicalJson(value: unknown): string {
  return JSON.stringify(canonicalValue(value));
}

export async function sha256(value: unknown): Promise<string> {
  const digest = await crypto.subtle.digest("SHA-256", encoder.encode(canonicalJson(value)));
  return bytesToBase64(new Uint8Array(digest));
}

function unsignedMessage(message: SignedMessage): Omit<SignedMessage, "signature"> {
  const { signature: _signature, ...unsigned } = message;
  return unsigned;
}

export class ValidatorNode {
  readonly validatorId: string;
  readonly genesis: Genesis;
  readonly store: Store;
  readonly publicKey: string;

  private readonly secretKey: Uint8Array;
  private readonly fetcher: typeof fetch;
  private readonly now: () => number;
  private readonly logger: Pick<Console, "log" | "warn" | "error">;
  private readonly validators: Map<string, ValidatorRecord>;

  constructor(options: NodeOptions) {
    this.validatorId = options.validatorId;
    this.genesis = options.genesis;
    this.store = options.store;
    this.fetcher = options.fetcher ?? fetch;
    this.now = options.now ?? Date.now;
    this.logger = options.logger ?? console;
    this.validators = new Map(options.genesis.validators.map((v) => [v.id, v]));

    if (options.genesis.validators.length !== 6) throw new Error("genesis must contain exactly six validators");
    if (options.genesis.quorum !== 5) throw new Error("genesis quorum must be five");
    if (options.genesis.heartbeatMs !== 15000) throw new Error("heartbeat must be 15000ms");

    const seed = base64ToBytes(options.privateKey);
    if (seed.length !== nacl.sign.seedLength) throw new Error("VALIDATOR_PRIVATE_KEY must be a base64 32-byte seed");
    const pair = nacl.sign.keyPair.fromSeed(seed);
    this.secretKey = pair.secretKey;
    this.publicKey = bytesToBase64(pair.publicKey);

    const registered = this.validators.get(this.validatorId);
    if (!registered) throw new Error(`validator ${this.validatorId} is absent from genesis`);
    if (registered.publicKey !== this.publicKey) throw new Error("private key does not match genesis public key");
  }

  private validator(id: string): ValidatorRecord {
    const validator = this.validators.get(id);
    if (!validator) throw new Error(`unknown validator ${id}`);
    return validator;
  }

  private async sign(message: Omit<SignedMessage, "signature">): Promise<SignedMessage> {
    const signature = nacl.sign.detached(encoder.encode(canonicalJson(message)), this.secretKey);
    return { ...message, signature: bytesToBase64(signature) };
  }

  private verify(message: SignedMessage): boolean {
    try {
      const validator = this.validator(message.validatorId);
      return nacl.sign.detached.verify(
        encoder.encode(canonicalJson(unsignedMessage(message))),
        base64ToBytes(message.signature),
        base64ToBytes(validator.publicKey),
      );
    } catch {
      return false;
    }
  }

  private expectedProposer(height: number): ValidatorRecord {
    return this.genesis.validators[(height - 1) % this.genesis.validators.length];
  }

  async head(): Promise<Head> {
    return (await this.store.get<Head>("head")) ?? { height: 0, hash: "GENESIS", finalizedAt: 0 };
  }

  private currentRound(): number {
    return Math.floor(this.now() / this.genesis.heartbeatMs);
  }

  private async createProposal(height: number, previousHash: string): Promise<SignedMessage> {
    const round = this.currentRound();
    const txEntries = await this.store.list<Transaction>("mempool/", 100);
    const blockWithoutHash = {
      version: 1 as const,
      chainId: this.genesis.chainId,
      height,
      round,
      proposer: this.validatorId,
      previousHash,
      timestamp: this.now(),
      transactions: txEntries.map((entry) => entry.value),
    };
    const block: Block = { ...blockWithoutHash, hash: await sha256(blockWithoutHash) };
    return await this.sign({
      version: 1,
      network: this.genesis.network,
      type: "proposal",
      validatorId: this.validatorId,
      height,
      round,
      timestamp: this.now(),
      nonce: crypto.randomUUID(),
      payload: block,
    });
  }

  private async createVote(block: Block): Promise<SignedMessage> {
    return await this.sign({
      version: 1,
      network: this.genesis.network,
      type: "vote",
      validatorId: this.validatorId,
      height: block.height,
      round: block.round,
      timestamp: this.now(),
      nonce: crypto.randomUUID(),
      payload: { blockHash: block.hash, decision: "accept" },
    });
  }

  private validateEnvelope(message: SignedMessage): void {
    if (message.version !== 1 || message.network !== this.genesis.network) throw new Error("wrong protocol or network");
    if (!Number.isSafeInteger(message.height) || message.height < 1) throw new Error("invalid height");
    if (!Number.isSafeInteger(message.round) || message.round < 0) throw new Error("invalid round");
    if (Math.abs(this.now() - message.timestamp) > 10 * 60_000) throw new Error("message timestamp outside replay window");
    if (!this.verify(message)) throw new Error("invalid Ed25519 signature");
  }

  private async validateProposal(message: SignedMessage): Promise<Block> {
    const block = message.payload as Block;
    if (message.validatorId !== this.expectedProposer(message.height).id) throw new Error("unexpected proposer");
    if (block.proposer !== message.validatorId || block.height !== message.height || block.round !== message.round) {
      throw new Error("proposal envelope mismatch");
    }
    if (block.chainId !== this.genesis.chainId || block.version !== 1) throw new Error("wrong chain");
    const { hash, ...withoutHash } = block;
    if (await sha256(withoutHash) !== hash) throw new Error("invalid block hash");
    const head = await this.head();
    if (block.height === head.height + 1 && block.previousHash !== head.hash) throw new Error("previous hash mismatch");
    if (block.height > head.height + 1) throw new Error("node must catch up before accepting proposal");
    return block;
  }

  private async recordEquivocation(kind: MessageType, message: SignedMessage, digest: string): Promise<void> {
    const key = `choice/${kind}/${message.height}/${message.validatorId}`;
    const first = await this.store.get<string>(key);
    if (first && first !== digest) {
      await this.store.set(`evidence/${kind}/${message.height}/${message.validatorId}/${this.now()}`, { first, second: digest, message });
      throw new Error(`${kind} equivocation detected`);
    }
    if (!first) await this.store.putIfAbsent(key, digest);
  }

  async ingest(message: SignedMessage, relay = true): Promise<{ accepted: true; finalized?: Head }> {
    this.validateEnvelope(message);
    const replayKey = `replay/${message.validatorId}/${message.nonce}`;
    if (!(await this.store.putIfAbsent(replayKey, message.timestamp))) throw new Error("replayed message");

    const digest = await sha256(unsignedMessage(message));
    await this.recordEquivocation(message.type, message, message.type === "vote" ? (message.payload as VotePayload).blockHash : digest);
    await this.store.set(`message/${message.height}/${digest}`, message);

    if (message.type === "proposal") {
      const block = await this.validateProposal(message);
      await this.store.set(`proposal/${block.height}/${block.hash}`, message);
      const ownVoteKey = `vote/${block.height}/${block.hash}/${this.validatorId}`;
      if (!(await this.store.get<SignedMessage>(ownVoteKey))) {
        const vote = await this.createVote(block);
        await this.store.set(ownVoteKey, vote);
        await this.ingestOwnVote(vote);
        if (relay) await this.broadcast(vote);
      }
      if (relay) await this.broadcast(message);
      return { accepted: true, finalized: await this.tryFinalize(block.height, block.hash) };
    }

    const vote = message.payload as VotePayload;
    if (vote.decision !== "accept") throw new Error("unsupported vote decision");
    const proposal = await this.store.get<SignedMessage>(`proposal/${message.height}/${vote.blockHash}`);
    if (!proposal) throw new Error("vote references unknown proposal");
    await this.store.set(`vote/${message.height}/${vote.blockHash}/${message.validatorId}`, message);
    if (relay) await this.broadcast(message);
    return { accepted: true, finalized: await this.tryFinalize(message.height, vote.blockHash) };
  }

  private async ingestOwnVote(vote: SignedMessage): Promise<void> {
    const payload = vote.payload as VotePayload;
    await this.store.putIfAbsent(`replay/${vote.validatorId}/${vote.nonce}`, vote.timestamp);
    await this.recordEquivocation("vote", vote, payload.blockHash);
    await this.store.set(`vote/${vote.height}/${payload.blockHash}/${vote.validatorId}`, vote);
  }

  private async tryFinalize(height: number, blockHash: string): Promise<Head | undefined> {
    const votes = await this.store.list<SignedMessage>(`vote/${height}/${blockHash}/`, 10);
    const unique = new Set(votes.map((entry) => entry.value.validatorId));
    if (unique.size < this.genesis.quorum) return undefined;
    const proposal = await this.store.get<SignedMessage>(`proposal/${height}/${blockHash}`);
    if (!proposal) return undefined;
    const block = proposal.payload as Block;
    const head = await this.head();
    if (height <= head.height) return head;
    if (height !== head.height + 1 || block.previousHash !== head.hash) return undefined;
    const next: Head = { height, hash: blockHash, finalizedAt: this.now() };
    if (!(await this.store.advanceHead(head.height, next))) return undefined;
    await this.store.set(`block/${String(height).padStart(12, "0")}`, block);
    for (const tx of block.transactions) await this.store.set(`included/${tx.id}`, height);
    this.logger.log(JSON.stringify({ event: "finalized", validator: this.validatorId, height, hash: blockHash }));
    return next;
  }

  private async broadcast(message: SignedMessage): Promise<void> {
    const body = canonicalJson(message);
    await Promise.allSettled(this.genesis.validators
      .filter((validator) => validator.id !== this.validatorId)
      .map(async (validator) => {
        const response = await this.fetcher(`${validator.url}/messages`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body,
        });
        if (!response.ok && response.status !== 409) throw new Error(`${validator.id} returned ${response.status}`);
      }));
  }

  async catchUp(): Promise<void> {
    let head = await this.head();
    for (const peer of this.genesis.validators.filter((v) => v.id !== this.validatorId)) {
      try {
        const response = await this.fetcher(`${peer.url}/blocks?from=${head.height + 1}&limit=100`);
        if (!response.ok) continue;
        const blocks = await response.json() as Block[];
        for (const block of blocks) {
          if (block.height !== head.height + 1 || block.previousHash !== head.hash) break;
          const { hash, ...withoutHash } = block;
          if (await sha256(withoutHash) !== hash) break;
          const next = { height: block.height, hash: block.hash, finalizedAt: this.now() };
          if (await this.store.advanceHead(head.height, next)) {
            await this.store.set(`block/${String(block.height).padStart(12, "0")}`, block);
            head = next;
          }
        }
      } catch (error) {
        this.logger.warn(`catch-up from ${peer.id} failed`, error);
      }
    }
  }

  async tick(): Promise<{ head: Head; proposed: boolean }> {
    await this.catchUp();
    const head = await this.head();
    const height = head.height + 1;
    if (this.expectedProposer(height).id !== this.validatorId) return { head, proposed: false };
    const existing = await this.store.list<SignedMessage>(`proposal/${height}/`, 1);
    if (existing.length) return { head, proposed: false };
    const proposal = await this.createProposal(height, head.hash);
    await this.ingest(proposal, false);
    await this.broadcast(proposal);
    return { head: await this.head(), proposed: true };
  }

  async handle(request: Request, context: RuntimeContext): Promise<Response> {
    const url = new URL(request.url);
    const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), {
      status,
      headers: { "content-type": "application/json", "cache-control": "no-store" },
    });

    try {
      if (request.method === "GET" && url.pathname === "/health") return json({ ok: true, validatorId: this.validatorId, network: this.genesis.network });
      if (request.method === "GET" && url.pathname === "/status") {
        return json({ validatorId: this.validatorId, publicKey: this.publicKey, head: await this.head(), round: this.currentRound(), quorum: this.genesis.quorum });
      }
      if (request.method === "GET" && url.pathname === "/peers") return json(this.genesis.validators.map(({ id, publicKey, url }) => ({ id, publicKey, url })));
      if (request.method === "GET" && url.pathname === "/blocks") {
        const from = Math.max(1, Number(url.searchParams.get("from") ?? 1));
        const limit = Math.min(100, Math.max(1, Number(url.searchParams.get("limit") ?? 25)));
        const entries = await this.store.list<Block>("block/", 10000);
        return json(entries.map((entry) => entry.value).filter((block) => block.height >= from).slice(0, limit));
      }
      if (request.method === "GET" && url.pathname.startsWith("/blocks/")) {
        const height = Number(url.pathname.split("/").pop());
        const block = await this.store.get<Block>(`block/${String(height).padStart(12, "0")}`);
        return block ? json(block) : json({ error: "not found" }, 404);
      }
      if (request.method === "POST" && url.pathname === "/transactions") {
        const input = await request.json() as Partial<Transaction>;
        if (!input.id || !input.sender || !Number.isSafeInteger(input.nonce) || input.payload === undefined) return json({ error: "invalid transaction" }, 400);
        if (await this.store.get(`included/${input.id}`)) return json({ error: "already included" }, 409);
        const tx: Transaction = { id: input.id, sender: input.sender, nonce: input.nonce!, payload: input.payload, receivedAt: this.now() };
        if (!(await this.store.putIfAbsent(`mempool/${tx.id}`, tx))) return json({ error: "duplicate" }, 409);
        return json(tx, 202);
      }
      if (request.method === "POST" && url.pathname === "/messages") {
        const result = await this.ingest(await request.json() as SignedMessage);
        return json(result, 202);
      }
      if (request.method === "POST" && url.pathname === "/tick") {
        const task = this.tick();
        context.waitUntil(task);
        return json(await task, 202);
      }
      return json({ error: "not found" }, 404);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      this.logger.warn(JSON.stringify({ event: "request_error", validator: this.validatorId, path: url.pathname, message }));
      return json({ error: message }, message.includes("replay") || message.includes("equivocation") ? 409 : 400);
    }
  }
}

export const text = { encoder, decoder };
