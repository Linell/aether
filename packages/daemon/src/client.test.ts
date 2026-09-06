import { describe, expect, test } from "bun:test";
import { AetherHttpError, createClient } from "./client";

function fakeFetch(
  handler: (input: string, init: RequestInit) => Response,
): typeof fetch {
  return (async (input: Parameters<typeof fetch>[0], init?: RequestInit) => {
    return handler(String(input), init ?? {});
  }) as typeof fetch;
}

describe("createClient", () => {
  test("sends Bearer auth header", async () => {
    const captured: { auth: string | undefined } = { auth: undefined };
    const client = createClient({
      baseUrl: "http://example.test",
      token: "secret-token",
      fetch: fakeFetch((_input, init) => {
        const headers = init.headers as Record<string, string>;
        captured.auth = headers.Authorization;
        return new Response(JSON.stringify({ content: "hi", version: 1 }), { status: 200 });
      }),
    });

    await client.getMemory("foo");

    expect(captured.auth).toBe("Bearer secret-token");
  });

  test("500 response throws AetherHttpError", async () => {
    const client = createClient({
      baseUrl: "http://example.test",
      token: "secret-token",
      fetch: fakeFetch(() => new Response("boom", { status: 500 })),
    });

    await expect(client.getMemory("foo")).rejects.toBeInstanceOf(AetherHttpError);

    try {
      await client.getMemory("foo");
      throw new Error("expected rejection");
    } catch (err) {
      expect(err).toBeInstanceOf(AetherHttpError);
      expect((err as AetherHttpError).status).toBe(500);
      expect((err as AetherHttpError).body).toBe("boom");
    }
  });
});
