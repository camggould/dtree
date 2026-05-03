import {
  Card,
  CardBody,
  CardHeader,
  RadioGroup,
  Radio,
  Select,
  SelectItem,
  Spinner,
  Code,
} from "@heroui/react";
import { useAppStore } from "@/store/app";
import { useActors } from "@/api/query";

export function SettingsView() {
  const handle = useAppStore((s) => s.currentHandle);
  const setHandle = useAppStore((s) => s.setHandle);
  const theme = useAppStore((s) => s.theme);
  const setTheme = useAppStore((s) => s.setTheme);

  const actorsQuery = useActors();

  return (
    <div className="p-6 max-w-3xl mx-auto">
      <h1 className="text-2xl font-bold mb-6">Settings</h1>

      <div className="flex flex-col gap-4">
        <Card>
          <CardHeader>
            <h2 className="text-base font-semibold">Identity</h2>
          </CardHeader>
          <CardBody className="gap-3">
            <p className="text-sm text-default-500">
              Mutations the UI sends are attributed to this handle. Read-only
              browsing works without one.
            </p>
            {actorsQuery.isLoading ? (
              <Spinner size="sm" />
            ) : (
              <Select
                label="Acting as"
                selectedKeys={handle ? new Set([handle]) : new Set()}
                onSelectionChange={(keys) => {
                  const k = Array.from(keys, String)[0];
                  setHandle(k ?? null);
                }}
                placeholder="No identity selected"
              >
                {(actorsQuery.data ?? []).map((a) => (
                  <SelectItem key={a.handle}>
                    {a.handle} — {a.name ?? a.display_name} ({a.kind})
                  </SelectItem>
                ))}
              </Select>
            )}
          </CardBody>
        </Card>

        <Card>
          <CardHeader>
            <h2 className="text-base font-semibold">Appearance</h2>
          </CardHeader>
          <CardBody>
            <RadioGroup
              label="Theme"
              orientation="horizontal"
              value={theme}
              onValueChange={(v) =>
                setTheme(v as "light" | "dark" | "system")
              }
            >
              <Radio value="light">Light</Radio>
              <Radio value="dark">Dark</Radio>
              <Radio value="system">System</Radio>
            </RadioGroup>
          </CardBody>
        </Card>

        <Card>
          <CardHeader>
            <h2 className="text-base font-semibold">About</h2>
          </CardHeader>
          <CardBody className="gap-2 text-sm">
            <div className="flex gap-2">
              <span className="text-default-500 w-32">API base</span>
              <Code size="sm">/v1</Code>
            </div>
            <div className="flex gap-2">
              <span className="text-default-500 w-32">UI base</span>
              <Code size="sm">/ui</Code>
            </div>
            <p className="text-default-500 mt-2">
              Local state (identity, theme, last-viewed tree) is persisted to
              localStorage under <Code size="sm">dtree-app</Code>.
            </p>
          </CardBody>
        </Card>
      </div>
    </div>
  );
}

export default SettingsView;
