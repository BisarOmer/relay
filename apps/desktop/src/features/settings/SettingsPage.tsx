import { RotateCcw } from "lucide-react";
import { useEffect, useState } from "react";
import { SHORTCUTS } from "@/app/Shortcuts";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Field, Input, Segmented } from "@/components/ui/input";
import { Banner, Card, Kbd, KeyValue, PageHeader, SectionHeader } from "@/components/ui/misc";
import { useAppStore } from "@/stores/app-store";
import { confirm } from "@/stores/ui-store";
import { DEFAULT_PREFERENCES, type Preferences } from "@/types/models";

export function SettingsPage() {
  const prefs = useAppStore((s) => s.preferences);
  const update = useAppStore((s) => s.updatePreferences);
  const info = useAppStore((s) => s.engineInfo);
  const engineState = useAppStore((s) => s.engineState);
  const restart = useAppStore((s) => s.restartEngine);
  const [maxMsgs, setMaxMsgs] = useState(String(prefs.maxDisplayedMessages));

  useEffect(() => setMaxMsgs(String(prefs.maxDisplayedMessages)), [prefs.maxDisplayedMessages]);

  const maxNum = Number(maxMsgs);
  const maxError = !Number.isInteger(maxNum) || maxNum < 100 || maxNum > 20000 ? "Between 100 and 20000" : null;

  return (
    <div className="h-full overflow-auto">
      <PageHeader title="Settings" description="Preferences are saved in the local Relay database and apply immediately." />
      <div className="mx-auto max-w-2xl space-y-5 p-6">
        <Card className="space-y-4 p-4">
          <SectionHeader title="Appearance" />
          <Field label="Theme">
            <Segmented<Preferences["theme"]>
              ariaLabel="Theme"
              value={prefs.theme}
              onChange={(theme) => void update({ theme })}
              options={[
                { value: "dark", label: "Dark" },
                { value: "light", label: "Light" },
                { value: "system", label: "System" },
              ]}
            />
          </Field>
          <Field label="Density" hint="Compact reduces row heights in tables and lists.">
            <Segmented<Preferences["density"]>
              ariaLabel="Density"
              value={prefs.density}
              onChange={(density) => void update({ density })}
              options={[
                { value: "comfortable", label: "Comfortable" },
                { value: "compact", label: "Compact" },
              ]}
            />
          </Field>
          <Field label="Timestamps" hint="Applies to message lists, findings and charts.">
            <Segmented<Preferences["timezone"]>
              ariaLabel="Timezone"
              value={prefs.timezone}
              onChange={(timezone) => void update({ timezone })}
              options={[
                { value: "local", label: "Local time" },
                { value: "utc", label: "UTC" },
              ]}
            />
          </Field>
        </Card>

        <Card className="space-y-4 p-4">
          <SectionHeader title="Live messages" />
          <form
            className="flex items-end gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              if (!maxError) void update({ maxDisplayedMessages: maxNum });
            }}
          >
            <Field
              label="Messages kept in the inspector"
              htmlFor="set-max"
              error={maxError}
              hint="Older messages are discarded from the view (not from the broker) once this many are buffered."
              className="flex-1"
            >
              <Input id="set-max" inputMode="numeric" className="tabular max-w-[160px]" value={maxMsgs} onChange={(e) => setMaxMsgs(e.target.value)} />
            </Field>
            <Button type="submit" variant="secondary" disabled={Boolean(maxError) || maxNum === prefs.maxDisplayedMessages}>
              Apply
            </Button>
          </form>
        </Card>

        <Card className="space-y-4 p-4">
          <SectionHeader
            title="Engine"
            actions={
              <Button
                size="xs"
                variant="outline"
                disabled={engineState === "unavailable"}
                onClick={async () => {
                  const ok = await confirm({
                    title: "Restart the engine?",
                    description: "All broker connections and subscriptions close. Saved connections and Insights history are kept.",
                    confirmLabel: "Restart",
                  });
                  if (ok) void restart();
                }}
              >
                <RotateCcw className="h-3 w-3" /> Restart
              </Button>
            }
          />
          {info?.keystore === "file" && (
            <Banner tone="warning" className="rounded-md">
              The OS keychain is unavailable, so secrets are stored in owner-only files inside the data directory. They are not encrypted at rest.
            </Banner>
          )}
          <KeyValue
            items={[
              ["Status", <Badge tone={engineState === "ready" ? "success" : engineState === "starting" ? "warning" : "danger"} dot>{engineState}</Badge>],
              ["Version", info?.engineVersion],
              ["Database schema", info?.schemaVersion !== undefined ? `v${info.schemaVersion}` : undefined],
              ["Secret storage", info?.keystore === "os" ? "OS keychain" : info?.keystore === "file" ? "File fallback" : undefined],
              ["Data directory", info?.dataDir ? <span className="break-all font-mono text-[11px]">{info.dataDir}</span> : undefined],
            ]}
          />
        </Card>

        <Card className="p-4">
          <SectionHeader title="Keyboard shortcuts" className="mb-3" />
          <ul className="grid gap-1.5 sm:grid-cols-2">
            {SHORTCUTS.map((s) => (
              <li key={s.label} className="flex items-center justify-between gap-3 text-xs">
                <span className="text-fg-muted">{s.label}</span>
                <span className="flex gap-1">
                  {s.keys.map((k) => (
                    <Kbd key={k}>{k}</Kbd>
                  ))}
                </span>
              </li>
            ))}
          </ul>
        </Card>

        <div className="flex justify-end">
          <Button
            variant="ghost"
            size="sm"
            onClick={async () => {
              if (await confirm({ title: "Reset preferences?", description: "Theme, density, timestamps and buffer size return to their defaults.", confirmLabel: "Reset" }))
                void update({ ...DEFAULT_PREFERENCES, sidebarCollapsed: prefs.sidebarCollapsed });
            }}
          >
            Reset preferences
          </Button>
        </div>
      </div>
    </div>
  );
}
