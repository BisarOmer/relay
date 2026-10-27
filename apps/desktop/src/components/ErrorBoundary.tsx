import { AlertOctagon } from "lucide-react";
import { Component, type ErrorInfo, type ReactNode } from "react";
import { Button } from "@/components/ui/button";

interface Props {
  children: ReactNode;
  /** Changing this value clears the error (e.g. the active page or tab id). */
  resetKey?: unknown;
  label?: string;
}

interface State {
  error: Error | null;
}

/** Contains render errors to one region so the rest of the app keeps working. */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null };

  static getDerivedStateFromError(error: Error): State {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(`[relay] ${this.props.label ?? "view"} crashed`, error, info.componentStack);
  }

  componentDidUpdate(prev: Props) {
    if (this.state.error && prev.resetKey !== this.props.resetKey) this.setState({ error: null });
  }

  render() {
    if (!this.state.error) return this.props.children;
    return (
      <div role="alert" className="flex h-full flex-col items-center justify-center gap-3 p-10 text-center">
        <AlertOctagon className="h-6 w-6 text-danger" />
        <div className="max-w-md space-y-1">
          <div className="text-[13px] font-medium">This {this.props.label ?? "view"} hit an unexpected error</div>
          <div className="break-words font-mono text-[11px] text-fg-muted">{this.state.error.message}</div>
        </div>
        <Button size="sm" variant="secondary" onClick={() => this.setState({ error: null })}>
          Try again
        </Button>
      </div>
    );
  }
}
