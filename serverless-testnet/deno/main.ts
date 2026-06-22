import { Genesis, Head, RuntimeContext, Store, ValidatorNode } from "../core.ts";

class DenoKvStore implements Store {
  constructor(private readonly kv: Deno.Kv) {}

  private key(value: string): Deno.KvKey {
    return value.split("/").filter(Boolean);
  }

  async get<T>(key: string): Promise<T | null> {
    return (await this.kv.get<T>(this.key(key), { consistency: "strong" })).value;
  }

  async set<T>(key: string, value: T): Promise<void> {
    await this.kv.set(this.key(key), value);
  }

  async list<T>(prefix: string, limit = 1000): Promise<Array<{ key: string; value: T }>> {
    const result: Array<{ key: string; value: T }> = [];
    for await (const entry of this.kv.list<T>({ prefix: this.key(prefix) }, { limit, consistency: "strong" })) {
      result.push({ key: entry.key.join("/"), value: entry.value });
    }
    return result;
  }

  async putIfAbsent<T>(key: string, value: T): Promise<boolean> {
    const kvKey = this.key(key);
    const current = await this.kv.get(kvKey, { consistency: "strong" });
    return (await this.kv.atomic().check(current).set(kvKey, value).commit()).ok;
  }

  async advanceHead(expectedHeight: number, head: Head): Promise<boolean> {
    const key = this.key("head");
    const current = await this.kv.get<Head>(key, { consistency: "strong" });
    if ((current.value?.height ?? 0) !== expectedHeight) return false;
    return (await this.kv.atomic().check(current).set(key, head).commit()).ok;
  }
}

const validatorId = Deno.env.get("VALIDATOR_ID");
const privateKey = Deno.env.get("VALIDATOR_PRIVATE_KEY");
const genesisJson = Deno.env.get("GENESIS_JSON");
if (!validatorId || !privateKey || !genesisJson) {
  throw new Error("VALIDATOR_ID, VALIDATOR_PRIVATE_KEY, and GENESIS_JSON are required");
}

const kv = await Deno.openKv();
const node = new ValidatorNode({
  validatorId,
  privateKey,
  genesis: JSON.parse(genesisJson) as Genesis,
  store: new DenoKvStore(kv),
});

Deno.cron("synthos-validator-safety-tick", "* * * * *", async () => {
  try {
    await node.tick();
  } catch (error) {
    console.error(JSON.stringify({ event: "safety_tick_error", validatorId, error: String(error) }));
  }
});

Deno.serve((request) => {
  const context: RuntimeContext = {
    waitUntil(promise) {
      promise.catch((error) => console.error(JSON.stringify({ event: "background_error", validatorId, error: String(error) })));
    },
  };
  return node.handle(request, context);
});
