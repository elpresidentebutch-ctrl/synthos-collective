import nacl from "npm:tweetnacl@1.0.3";
import { assertEquals } from "jsr:@std/assert@1";
import { Genesis, Head, Store, ValidatorNode } from "./core.ts";

class MemoryStore implements Store {
  values = new Map<string, unknown>();
  async get<T>(key: string): Promise<T | null> { return (this.values.get(key) as T | undefined) ?? null; }
  async set<T>(key: string, value: T): Promise<void> { this.values.set(key, value); }
  async list<T>(prefix: string, limit = 1000): Promise<Array<{ key: string; value: T }>> {
    return Array.from(this.values.entries()).filter(([key]) => key.startsWith(prefix)).sort(([a], [b]) => a.localeCompare(b)).slice(0, limit)
      .map(([key, value]) => ({ key, value: value as T }));
  }
  async putIfAbsent<T>(key: string, value: T): Promise<boolean> {
    if (this.values.has(key)) return false;
    this.values.set(key, value);
    return true;
  }
  async advanceHead(expectedHeight: number, head: Head): Promise<boolean> {
    const current = await this.get<Head>("head") ?? { height: 0, hash: "GENESIS", finalizedAt: 0 };
    if (current.height !== expectedHeight) return false;
    this.values.set("head", head);
    return true;
  }
}

function b64(bytes: Uint8Array): string {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

Deno.test("six validators finalize with five-of-six quorum", async () => {
  const ids = ["cloudflare-1", "deno-1", "deno-2", "deno-3", "deno-4", "deno-5"];
  const pairs = ids.map(() => nacl.sign.keyPair());
  const genesis: Genesis = {
    protocolVersion: 1,
    chainId: "synthos-testnet-test",
    network: "synthos-testnet-test",
    genesisTime: new Date(0).toISOString(),
    heartbeatMs: 15000,
    quorum: 5,
    validators: ids.map((id, index) => ({ id, publicKey: b64(pairs[index].publicKey), url: `https://${id}.test` })),
    allocations: {},
  };
  const nodes = new Map<string, ValidatorNode>();
  const stores = new Map<string, MemoryStore>();
  let now = 30_000;

  const fetcher: typeof fetch = async (input, init) => {
    const request = new Request(input, init);
    const url = new URL(request.url);
    const node = nodes.get(url.hostname.replace(".test", ""));
    if (!node) return new Response("missing node", { status: 404 });
    return await node.handle(request, { waitUntil: () => undefined });
  };

  ids.forEach((id, index) => {
    const store = new MemoryStore();
    stores.set(id, store);
    nodes.set(id, new ValidatorNode({
      validatorId: id,
      privateKey: b64(pairs[index].secretKey.slice(0, 32)),
      genesis,
      store,
      fetcher,
      now: () => now,
      logger: { log() {}, warn() {}, error() {} },
    }));
  });

  await nodes.get("cloudflare-1")!.tick();
  await new Promise((resolve) => setTimeout(resolve, 25));

  const heads = await Promise.all(ids.map((id) => nodes.get(id)!.head()));
  assertEquals(heads.map((head) => head.height), [1, 1, 1, 1, 1, 1]);
  assertEquals(new Set(heads.map((head) => head.hash)).size, 1);

  now += 15_000;
  await nodes.get("deno-1")!.tick();
  await new Promise((resolve) => setTimeout(resolve, 25));
  const secondHeads = await Promise.all(ids.map((id) => nodes.get(id)!.head()));
  assertEquals(secondHeads.map((head) => head.height), [2, 2, 2, 2, 2, 2]);
});
