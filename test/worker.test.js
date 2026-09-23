import { env, exports } from "cloudflare:workers";
import { describe, expect, it } from "vitest";

const SYNC_SALT = "KISS-Translator-SYNC";
const SHARE_SALT = "KISS-Translator-SHARE";

async function digest(text, salt) {
  const bytes = new TextEncoder().encode(text + salt);
  const hash = await crypto.subtle.digest("SHA-256", bytes);
  return [...new Uint8Array(hash)]
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
}

async function sync(data) {
  return exports.default.fetch("https://worker.test/sync", {
    method: "POST",
    headers: {
      "content-type": "application/json",
      Authorization: `Bearer ${await digest(env.AUTH_VALUE, SYNC_SALT)}`,
    },
    body: JSON.stringify(data),
  });
}

function uniqueKey(prefix) {
  return `${prefix}-${crypto.randomUUID()}`;
}

describe("KISS Worker protocol", () => {
  it("preserves the existing sync request and response shape", async () => {
    const key = uniqueKey("setting");
    const value = JSON.stringify({ encrypted: true, data: "opaque" });
    const response = await sync({ key, value, updateAt: 0 });
    expect(response.status).toBe(200);
    const result = await response.json();
    expect(result).toEqual({ key, value, updateAt: expect.any(Number) });
    expect(result.updateAt).toBeGreaterThan(100_000_000_000);
  });

  it("allows an empty opaque value", async () => {
    const key = uniqueKey("empty");
    const response = await sync({ key, value: "", updateAt: 1_800_000_000_000 });
    expect(await response.json()).toEqual({
      key,
      value: "",
      updateAt: 1_800_000_000_000,
    });
  });

  it("returns the newest record under concurrent out-of-order requests", async () => {
    const key = uniqueKey("concurrent");
    const base = 1_800_000_000_000;
    await Promise.all(
      Array.from({ length: 24 }, (_, index) =>
        sync({ key, value: `value-${index}`, updateAt: base + index })
      )
    );
    const response = await sync({ key, value: "local", updateAt: 0 });
    expect(await response.json()).toEqual({
      key,
      value: "value-23",
      updateAt: base + 23,
    });
  });

  it("lazily migrates existing KV data and normalizes legacy seconds", async () => {
    const key = uniqueKey("legacy");
    await env.KV.put(key, "legacy-value", {
      metadata: { updateAt: 1_700_000_000 },
    });
    const response = await sync({ key, value: "local", updateAt: 0 });
    expect(await response.json()).toEqual({
      key,
      value: "legacy-value",
      updateAt: 1_700_000_000_000,
    });
  });

  it("keeps the existing rules sharing URL protocol", async () => {
    const rules = [{ pattern: "example.com" }];
    await env.KV.put("kiss-rules-share.json", JSON.stringify(rules), {
      metadata: { updateAt: 1_800_000_000_000 },
    });
    const psk = await digest(env.AUTH_VALUE, SHARE_SALT);
    const response = await exports.default.fetch(
      `https://worker.test/rules?psk=${psk}`
    );
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual(rules);
  });

  it("rejects malformed fields without exposing internal errors", async () => {
    const response = await sync({
      hasOwnProperty: 0,
      key: uniqueKey("malformed"),
      value: {},
      updateAt: -1,
    });
    expect(response.status).toBe(400);
    expect(await response.text()).toBe("Fields Error.");
  });

  it("keeps the existing authorization failure response", async () => {
    const response = await exports.default.fetch("https://worker.test/sync", {
      method: "POST",
      headers: { Authorization: "Bearer invalid" },
      body: "{}",
    });
    expect(response.status).toBe(403);
    expect(await response.text()).toBe(
      "Sorry, you have supplied an invalid key."
    );
  });
});
