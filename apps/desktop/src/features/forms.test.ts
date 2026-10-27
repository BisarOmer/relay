import { describe, expect, it } from "vitest";
import { fuzzyScore } from "./command-palette/CommandPalette";
import { EMPTY_FORM, validateForm, type FormState } from "./connections/ConnectionForm";
import { NEW_SYSTEM_FORM, validateSystemForm } from "./insights/SystemsPane";

const conn = (patch: Partial<FormState>): FormState => ({ ...EMPTY_FORM, name: "Local", ...patch });

describe("connection form validation", () => {
  it("accepts the defaults with a name", () => {
    expect(validateForm(conn({}), false)).toEqual({});
  });

  it("rejects credentials embedded in a server URL", () => {
    expect(validateForm(conn({ servers: "nats://alice:hunter2@127.0.0.1:4222" }), false).servers).toMatch(/credentials/i);
  });

  it("rejects unsupported schemes and accepts the supported ones", () => {
    expect(validateForm(conn({ servers: "http://127.0.0.1:4222" }), false).servers).toMatch(/Unsupported scheme/);
    expect(validateForm(conn({ servers: "nats://a:4222\ntls://b:4222, ws://c:80\nwss://d:443" }), false).servers).toBeUndefined();
    expect(validateForm(conn({ servers: "127.0.0.1:4222" }), false).servers).toBeUndefined();
  });

  it("mirrors the engine's numeric bounds", () => {
    expect(validateForm(conn({ timeoutMs: "499" }), false).timeoutMs).toBeDefined();
    expect(validateForm(conn({ timeoutMs: "120000" }), false).timeoutMs).toBeUndefined();
    expect(validateForm(conn({ maxBackoffMs: "300001" }), false).maxBackoffMs).toBeDefined();
    expect(validateForm(conn({ initialBackoffMs: "2000", maxBackoffMs: "1000" }), false).maxBackoffMs).toMatch(/at least/);
    expect(validateForm(conn({ maxAttempts: "100001" }), false).maxAttempts).toBeDefined();
    expect(validateForm(conn({ reconnectEnabled: false, maxAttempts: "x" }), false).maxAttempts).toBeUndefined();
  });

  it("requires a secret only when none is stored or it is being cleared", () => {
    const tokenForm = conn({ authMethod: "token" });
    expect(validateForm(tokenForm, false).secret).toMatch(/Token is required/);
    expect(validateForm(tokenForm, true).secret).toBeUndefined();
    expect(validateForm({ ...tokenForm, clearSecret: true }, true).secret).toMatch(/Token is required/);
  });

  it("checks the nkey seed shape", () => {
    expect(validateForm(conn({ authMethod: "nkey", secret: "not-a-seed" }), false).secret).toMatch(/SU/);
  });

  it("requires a name for a custom environment", () => {
    expect(validateForm(conn({ environment: "custom" }), false).environmentName).toBeDefined();
  });
});

describe("monitored system form validation", () => {
  const sys = (patch: Partial<typeof NEW_SYSTEM_FORM>) => ({ ...NEW_SYSTEM_FORM, name: "Local", ...patch });

  it("accepts the defaults with a name", () => {
    expect(validateSystemForm(sys({}))).toEqual({});
  });

  it("only allows http(s) URLs without credentials", () => {
    expect(validateSystemForm(sys({ url: "ftp://127.0.0.1:8222" })).url).toMatch(/http/);
    expect(validateSystemForm(sys({ url: "http://admin:secret@127.0.0.1:8222" })).url).toMatch(/Credentials/);
    expect(validateSystemForm(sys({ url: "not a url" })).url).toBeDefined();
  });

  it("does not require a URL for demo systems", () => {
    expect(validateSystemForm(sys({ demo: true, url: "" })).url).toBeUndefined();
  });

  it("keeps the timeout within the interval", () => {
    expect(validateSystemForm(sys({ intervalSec: "1", timeoutMs: "1500" })).timeoutMs).toBeDefined();
    expect(validateSystemForm(sys({ intervalSec: "0" })).intervalSec).toBeDefined();
    expect(validateSystemForm(sys({ retention: "5" })).retention).toBeDefined();
  });
});

describe("command palette matching", () => {
  it("ranks substring matches before subsequence matches", () => {
    const sub = fuzzyScore("mess", "Live messages")!;
    const seq = fuzzyScore("lms", "Live messages")!;
    expect(sub).toBe(5);
    expect(seq).toBeGreaterThanOrEqual(100);
    expect(sub).toBeLessThan(seq);
  });

  it("returns null when characters are missing", () => {
    expect(fuzzyScore("xyz", "Insights")).toBeNull();
  });

  it("matches across words", () => {
    expect(fuzzyScore("disc loc", "Disconnect Local standalone")).not.toBeNull();
  });
});
