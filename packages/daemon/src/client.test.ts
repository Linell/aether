import { describe, expect, test } from "bun:test";
import { AetherHttpError, createClient } from "./client";

function clientWith(handler: (input: string, init: RequestInit) => Response) {
  const fakeFetch = (async (input: Parameters<typeof fetch>[0], init?: RequestInit) =>
    handler(String(input), init ?? {})) as typeof fetch;
  return createClient({ baseUrl: "http://example.test", token: "secret-token", fetch: fakeFetch });
}

describe("createClient", () => {
  test("sends Bearer auth header to the v1 route", async () => {
    let seen: { url: string; auth: string | undefined } | undefined;
    const client = clientWith((url, init) => {
      seen = { url, auth: (init.headers as Record<string, string>).Authorization };
      return new Response(JSON.stringify({ content: "hi", version: 1 }), { status: 200 });
    });

    await client.getMemory("foo");

    expect(seen).toEqual({
      url: "http://example.test/v1/daemons/foo/memory",
      auth: "Bearer secret-token",
    });
  });

  test("non-2xx response throws AetherHttpError", async () => {
    const client = clientWith(() => new Response("boom", { status: 500 }));

    const err = await client.getMemory("foo").catch((e: unknown) => e);

    expect(err).toBeInstanceOf(AetherHttpError);
    expect((err as AetherHttpError).status).toBe(500);
    expect((err as AetherHttpError).body).toBe("boom");
  });
});
