import { describe, expect, it } from "vitest";
import { decodePayload, encodePayload, tryPrettyJson } from "./utils";

describe("payload helpers", () => {
  it("round-trips utf-8 payloads", () => {
    const original = '{"hello":"relay"}';
    expect(decodePayload(encodePayload(original))).toBe(original);
  });

  it("pretty-prints json", () => {
    expect(tryPrettyJson('{"a":1}')).toContain("\n");
  });
});
