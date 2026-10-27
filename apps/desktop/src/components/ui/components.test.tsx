import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { useUiStore } from "@/stores/ui-store";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { LineChart } from "./chart";
import { Stat } from "./misc";
import { ConfirmHost } from "./overlays";

describe("Stat", () => {
  it("shows an unknown value as a dash with its reason, never as zero", () => {
    render(<Stat label="Streams" value={null} unknownReason="JetStream is disabled on this server" />);
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.getByText("JetStream is disabled on this server")).toBeInTheDocument();
    expect(screen.queryByText("0")).not.toBeInTheDocument();
  });

  it("shows a real zero as zero", () => {
    render(<Stat label="Slow consumers" value={0} sub="cumulative" />);
    expect(screen.getByText("0")).toBeInTheDocument();
    expect(screen.getByText("cumulative")).toBeInTheDocument();
  });
});

describe("ConfirmHost", () => {
  it("resolves false on cancel and true on confirm", async () => {
    const user = userEvent.setup();
    render(<ConfirmHost />);
    let p!: Promise<boolean>;
    act(() => {
      p = useUiStore.getState().requestConfirm({ title: "Publish to production?", confirmLabel: "Publish" });
    });
    await user.click(await screen.findByRole("button", { name: "Cancel" }));
    await expect(p).resolves.toBe(false);

    act(() => {
      p = useUiStore.getState().requestConfirm({ title: "Publish to production?", confirmLabel: "Publish" });
    });
    await user.click(await screen.findByRole("button", { name: "Publish" }));
    await expect(p).resolves.toBe(true);
  });

  it("keeps confirm disabled until the exact text is typed", async () => {
    const user = userEvent.setup();
    render(<ConfirmHost />);
    let p!: Promise<boolean>;
    act(() => {
      p = useUiStore.getState().requestConfirm({ title: "Delete prod?", confirmLabel: "Delete", requireText: "prod-east" });
    });
    const confirm = await screen.findByRole("button", { name: "Delete" });
    expect(confirm).toBeDisabled();
    const input = screen.getByLabelText(/to confirm/);
    await user.type(input, "prod-eas");
    expect(confirm).toBeDisabled();
    await user.type(input, "t");
    expect(confirm).toBeEnabled();
    await user.click(confirm);
    await expect(p).resolves.toBe(true);
  });
});

describe("LineChart", () => {
  const original = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "clientWidth");
  beforeAll(() => {
    Object.defineProperty(HTMLElement.prototype, "clientWidth", { configurable: true, get: () => 400 });
  });
  afterAll(() => {
    if (original) Object.defineProperty(HTMLElement.prototype, "clientWidth", original);
  });
  const at = (s: number) => new Date(Date.UTC(2026, 0, 1, 0, 0, s)).toISOString();

  it("renders an empty state without samples", () => {
    render(<LineChart ariaLabel="mem" points={[]} emptyLabel="No samples yet" />);
    expect(screen.getByText("No samples yet")).toBeInTheDocument();
  });

  it("breaks the line at unknown samples and marks the gap", () => {
    const { container } = render(
      <LineChart
        ariaLabel="in"
        points={[
          { at: at(0), value: 1 },
          { at: at(5), value: 2 },
          { at: at(10), value: null },
          { at: at(15), value: 3 },
          { at: at(20), value: 4 },
        ]}
      />,
    );
    expect(screen.getByRole("img", { name: "in" })).toBeInTheDocument();
    expect(container.querySelectorAll("path")).toHaveLength(2);
    expect(container.querySelectorAll("rect")).toHaveLength(1);
  });

  it("does not repeat identical axis labels for a flat series", () => {
    const { container } = render(<LineChart ariaLabel="c" points={[{ at: at(0), value: 2 }, { at: at(5), value: 2 }]} format={(v) => String(Math.round(v))} />);
    const labels = [...container.querySelectorAll("text")].map((t) => t.textContent).filter((t) => /^\d+$/.test(t ?? ""));
    expect(labels).toEqual(["1", "2", "3"]);
  });
});

describe("ErrorBoundary", () => {
  function Boom(): never {
    throw new Error("render failed");
  }

  it("contains a render error and shows it", () => {
    const err = console.error;
    console.error = () => {};
    try {
      render(
        <ErrorBoundary label="tab">
          <Boom />
        </ErrorBoundary>,
      );
    } finally {
      console.error = err;
    }
    expect(screen.getByRole("alert")).toHaveTextContent("This tab hit an unexpected error");
    expect(screen.getByText("render failed")).toBeInTheDocument();
  });
});
