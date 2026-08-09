import { RuntimeContext, SignedMessage, Store, ValidatorNode, VotePayload } from "./core.ts";

export class ReliableValidatorNode {
  constructor(private readonly node: ValidatorNode, private readonly store: Store) {}

  get validatorId(): string { return this.node.validatorId; }
  head() { return this.node.head(); }
  tick() { return this.node.tick(); }

  async handle(request: Request, context: RuntimeContext): Promise<Response> {
    const url = new URL(request.url);
    if (request.method !== "POST" || url.pathname !== "/messages") {
      return await this.node.handle(request, context);
    }

    const message = await request.clone().json() as SignedMessage;
    if (message.type === "vote") {
      const blockHash = (message.payload as VotePayload).blockHash;
      const proposal = await this.store.get(`proposal/${message.height}/${blockHash}`);
      if (!proposal) {
        await this.store.set(`pending-vote/${message.height}/${blockHash}/${message.validatorId}`, message);
        return new Response(JSON.stringify({ accepted: true, pending: true }), {
          status: 202,
          headers: { "content-type": "application/json" },
        });
      }
      return await this.node.handle(request, context);
    }

    const response = await this.node.handle(request, context);
    if (response.ok && message.type === "proposal") {
      const blockHash = (message.payload as { hash: string }).hash;
      const pending = await this.store.list<SignedMessage | null>(`pending-vote/${message.height}/${blockHash}/`, 10);
      for (const entry of pending) {
        if (!entry.value) continue;
        await this.node.handle(new Request("https://validator.internal/messages", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify(entry.value),
        }), context);
        await this.store.set(entry.key, null);
      }
    }
    return response;
  }
}
