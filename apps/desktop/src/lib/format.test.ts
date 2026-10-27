import { describe, expect, it } from "vitest";
import { counterRates, fmtBytes, fmtNumber, isKnown, staleness, toCsv } from "./format";
import { validateSubject } from "./utils";

const at = (s: number) => new Date(Date.UTC(2026, 0, 1, 0, 0, s)).toISOString();

describe("isKnown / formatting", () => {
  it("treats null, undefined and non-finite numbers as unknown, never as zero", () => {
    expect(isKnown(0)).toBe(true);
    expect(isKnown(null)).toBe(false);
    expect(isKnown(undefined)).toBe(false);
    expect(isKnown(Number.NaN)).toBe(false);
    expect(fmtNumber(null)).not.toBe("0");
    expect(fmtBytes(undefined)).not.toBe("0 B");
    expect(fmtNumber(0)).toBe("0");
  });

  it("formats bytes with binary units", () => {
    expect(fmtBytes(0)).toBe("0 B");
    expect(fmtBytes(1536)).toBe("1.50 KiB");
    expect(fmtBytes(25_900_000)).toMatch(/MiB$/);
  });
});

describe("counterRates", () => {
  it("derives per-second rates between consecutive samples", () => {
    const out = counterRates([
      { epochId: "a", at: at(0), value: 100 },
      { epochId: "b", at: at(5), value: 150 },
      { epochId: "c", at: at(10), value: 150 },
    ]);
    expect(out.map((p) => p.value)).toEqual([10, 0]);
    expect(out.map((p) => p.epochId)).toEqual(["b", "c"]);
  });

  it("turns a counter reset into a gap instead of a negative rate", () => {
    const out = counterRates([
      { epochId: "a", at: at(0), value: 500 },
      { epochId: "b", at: at(5), value: 20 },
    ]);
    expect(out).toEqual([{ epochId: "b", at: at(5), value: null }]);
  });

  it("produces a gap when either side of an interval is unknown", () => {
    const out = counterRates([
      { epochId: "a", at: at(0), value: 1 },
      { epochId: "b", at: at(5), value: null },
      { epochId: "c", at: at(10), value: 11 },
    ]);
    expect(out.map((p) => p.value)).toEqual([null, null]);
  });

  it("skips zero-length intervals rather than reporting them as gaps", () => {
    const out = counterRates([
      { epochId: "a", at: at(0), value: 1 },
      { epochId: "b", at: at(0), value: 1 },
      { epochId: "c", at: at(2), value: 5 },
    ]);
    expect(out.map((p) => p.value)).toEqual([2]);
  });

  it("needs two samples", () => {
    expect(counterRates([{ epochId: "a", at: at(0), value: 1 }])).toEqual([]);
  });
});

describe("staleness", () => {
  it("is stale when nothing was collected", () => {
    expect(staleness(null, 5000)).toEqual({ stale: true, ageMs: null });
  });

  it("allows three intervals (minimum 15 s) before calling data stale", () => {
    const now = Date.parse(at(60));
    expect(staleness(at(50), 5000, now).stale).toBe(false);
    expect(staleness(at(44), 5000, now).stale).toBe(true);
    expect(staleness(at(0), 30_000, now).stale).toBe(false);
  });
});

describe("toCsv", () => {
  it("escapes quotes, commas and newlines and leaves NULL empty", () => {
    const csv = toCsv(["a", "b"], [
      ['he said "hi"', "x,y"],
      [null, "line1\nline2"],
      [{ k: 1 }, 3],
    ]);
    const lines = csv.split("\n");
    expect(lines[0]).toBe("a,b");
    expect(lines[1]).toBe('"he said ""hi""","x,y"');
    expect(csv).toContain(',"line1\nline2"');
    expect(csv).toContain('"{""k"":1}",3');
  });
});

describe("validateSubject", () => {
  it("accepts plain and wildcard subjects where allowed", () => {
    expect(validateSubject("orders.created", false)).toBeNull();
    expect(validateSubject("orders.*.eu", true)).toBeNull();
    expect(validateSubject("orders.>", true)).toBeNull();
  });

  it("rejects wildcards when publishing", () => {
    expect(validateSubject("orders.>", false)).toMatch(/not allowed/);
  });

  it("rejects malformed subjects", () => {
    expect(validateSubject("", true)).toMatch(/required/);
    expect(validateSubject("a b", true)).toMatch(/whitespace/);
    expect(validateSubject("a..b", true)).toMatch(/empty token/);
    expect(validateSubject(".a", true)).toMatch(/empty token/);
    expect(validateSubject("a.>.b", true)).toMatch(/last token/);
    expect(validateSubject("a.b*", true)).toMatch(/whole token/);
  });
});
