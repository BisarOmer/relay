import { beforeEach, describe, expect, it } from "vitest";
import { useAppStore } from "./app-store";

const initial = useAppStore.getState();

describe("workspace tabs", () => {
  beforeEach(() => {
    useAppStore.setState(initial, true);
  });

  it("opening a connection's live tab does not also create a connection picker tab", () => {
    const { openTab } = useAppStore.getState();
    openTab({ id: "msgs-1", kind: "messages", title: "Local · live", connectionId: "1" });
    const { tabs, activity, activeTabId } = useAppStore.getState();
    expect(tabs.map((t) => t.id)).toEqual(["msgs-1"]);
    expect(activeTabId).toBe("msgs-1");
    expect(activity).toBe("messages");
  });

  it("keeps the highlighted activity in step with the active tab", () => {
    const { openTab, setActiveTab, closeTab } = useAppStore.getState();
    openTab({ id: "msgs-1", kind: "messages", title: "live", connectionId: "1" });
    openTab({ id: "pub-1", kind: "publisher", title: "publish", connectionId: "1" });
    expect(useAppStore.getState().activity).toBe("publisher");
    setActiveTab("msgs-1");
    expect(useAppStore.getState().activity).toBe("messages");
    openTab({ id: "conn-1", kind: "connection", title: "Local", connectionId: "1" });
    expect(useAppStore.getState().activity).toBe("explorer");
    closeTab("conn-1");
    expect(useAppStore.getState().activeTabId).toBe("pub-1");
    expect(useAppStore.getState().activity).toBe("publisher");
  });

  it("focuses an existing tab of the requested kind instead of opening another", () => {
    const { openTab, setActivity } = useAppStore.getState();
    openTab({ id: "msgs-1", kind: "messages", title: "live", connectionId: "1" });
    openTab({ id: "conn-1", kind: "connection", title: "Local", connectionId: "1" });
    setActivity("messages");
    const { tabs, activeTabId } = useAppStore.getState();
    expect(tabs).toHaveLength(2);
    expect(activeTabId).toBe("msgs-1");
  });

  it("falls back to the welcome tab when the last tab closes", () => {
    const { openTab, closeTab } = useAppStore.getState();
    openTab({ id: "conn-1", kind: "connection", title: "Local", connectionId: "1" });
    closeTab("conn-1");
    expect(useAppStore.getState().tabs.map((t) => t.kind)).toEqual(["welcome"]);
  });
});
