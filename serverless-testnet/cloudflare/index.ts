import { Genesis, Head, RuntimeContext, Store, ValidatorNode } from "../core.ts";

interface Env {
  VALIDATOR: DurableObjectNamespace;
  VALIDATOR_ID: string;
  VALIDATOR_PRIVATE_KEY: string;
  GENESIS_JSON: string;
}

class DurableStore implements Store {
  constructor(private readonly storage: DurableObjectStorage) {}

  async get<T>(key: string): Promise<T | null> {
    return (await this.storage.get<T>(key)) ?? null;
  }

  async set<T>(key: string, value: T): Promise<void> {
    await this.storage.put(key, value);
  }

  async list<T>(prefix: string, limit = 1000): Promise<Array<{ key: string; value: T }>> {
    const entries = await this.storage.list<T>({ prefix, limit });
    return Array.from(entries, ([key, value]) => ({ key, value }));
  }

  async putIfAbsent<T>(key: string, value: T): Promise<boolean> {
    if (await this.storage.get(key) !== undefined) return false;
    await this.storage.put(key, value);
    return true;
  }

  async advanceHead(expectedHeight: number, head: Head): Promise<boolean> {
    const current = (await this.get<Head>("head")) ?? { height: 0, hash: "GENESIS", finalizedAt: 0 };
    if (current.height !== expectedHeight) return false;
    await this.storage.put("head", head);
    return true;
  }
}

export class SynthosValidator implements DurableObject {
  private readonly node: ValidatorNode;
  private readonly genesis: Genesis;

  constructor(private readonly state: DurableObjectState, env: Env) {
    this.genesis = JSON.parse(env.GENESIS_JSON) as Genesis;
    this.node = new ValidatorNode({
      validatorId: env.VALIDATOR_ID,
      privateKey: env.VALIDATOR_PRIVATE_KEY,
      genesis: this.genesis,
      store: new DurableStore(state.storage),
    });
    state.blockConcurrencyWhile(async () => {
      if ((await state.storage.getAlarm()) === null) await state.storage.setAlarm(Date.now() + this.genesis.heartbeatMs);
    });
  }

  async fetch(request: Request): Promise<Response> {
    const context: RuntimeContext = { waitUntil: (promise) => this.state.waitUntil(promise) };
    return await this.node.handle(request, context);
  }

  async alarm(): Promise<void> {
    const started = Date.now();
    const results = await Promise.allSettled(this.genesis.validators.map(async (validator) => {
      const response = await fetch(`${validator.url}/tick`, { method: "POST", headers: { "x-synthos-scheduler": "cloudflare-do" } });
      if (!response.ok) throw new Error(`${validator.id}: HTTP ${response.status}`);
      return validator.id;
    }));
    console.log(JSON.stringify({
      event: "heartbeat",
      scheduler: this.node.validatorId,
      durationMs: Date.now() - started,
      fulfilled: results.filter((result) => result.status === "fulfilled").length,
      rejected: results.filter((result) => result.status === "rejected").length,
    }));
    await this.state.storage.setAlarm(Date.now() + this.genesis.heartbeatMs);
  }
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const id = env.VALIDATOR.idFromName(env.VALIDATOR_ID);
    return await env.VALIDATOR.get(id).fetch(request);
  },

  async scheduled(_controller: ScheduledController, env: Env, ctx: ExecutionContext): Promise<void> {
    const id = env.VALIDATOR.idFromName(env.VALIDATOR_ID);
    ctx.waitUntil(env.VALIDATOR.get(id).fetch("https://validator.internal/tick", { method: "POST" }).then(() => undefined));
  },
};
