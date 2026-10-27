import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { AppShell } from "@/app/AppShell";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import "@/styles/globals.css";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ErrorBoundary label="application">
      <AppShell />
    </ErrorBoundary>
  </StrictMode>,
);
