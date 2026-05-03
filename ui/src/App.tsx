import { HeroUIProvider } from "@heroui/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { Route, Switch } from "wouter";
import { queryClient } from "@/api/query";
import { useAppStore } from "@/store/app";
import { Layout } from "@/components/Layout";
import { useAuditStream } from "@/api/sse";
import GraphView from "@/views/GraphView";
import { DecisionView } from "@/views/DecisionView";
import { AuditView } from "@/views/AuditView";
import { QueueView } from "@/views/QueueView";
import { KanbanView } from "@/views/KanbanView";
import "@/styles/globals.css";

// Stub views
function HomeView() {
  return <div>Not implemented: HomeView</div>;
}
function TreeView() {
  return <GraphView />;
}
function DashboardView() {
  return <div>Not implemented: DashboardView</div>;
}
function ActorsView() {
  return <div>Not implemented: ActorsView</div>;
}
function SettingsView() {
  return <div>Not implemented: SettingsView</div>;
}
function NotFoundView() {
  return <div>Not implemented: NotFoundView</div>;
}

function AppRoutes() {
  // Open audit SSE stream globally
  useAuditStream();

  return (
    <Layout>
      <Switch>
        <Route path="/" component={HomeView} />
        <Route path="/trees/:tree" component={TreeView} />
        <Route path="/trees/:tree/decisions/:id" component={DecisionView} />
        <Route path="/trees/:tree/queue/:kind" component={QueueView} />
        <Route path="/trees/:tree/audit" component={AuditView} />
        <Route path="/trees/:tree/kanban" component={KanbanView} />
        <Route path="/dashboard" component={DashboardView} />
        <Route path="/actors" component={ActorsView} />
        <Route path="/settings" component={SettingsView} />
        <Route component={NotFoundView} />
      </Switch>
    </Layout>
  );
}

function ThemeWrapper({ children }: { children: React.ReactNode }) {
  const theme = useAppStore((s) => s.theme);
  const resolved =
    theme === "system"
      ? window.matchMedia("(prefers-color-scheme: dark)").matches
        ? "dark"
        : "light"
      : theme;

  return (
    <div data-theme={resolved} className={resolved}>
      {children}
    </div>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <HeroUIProvider>
        <ThemeWrapper>
          <AppRoutes />
        </ThemeWrapper>
      </HeroUIProvider>
    </QueryClientProvider>
  );
}
