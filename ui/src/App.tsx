import { useEffect } from "react";
import { HeroUIProvider } from "@heroui/react";
import { QueryClientProvider } from "@tanstack/react-query";
import { Route, Router, Switch } from "wouter";
import { queryClient } from "@/api/query";
import { useAppStore } from "@/store/app";
import { Layout } from "@/components/Layout";
import { ErrorBoundary } from "@/components/ErrorBoundary";
import { DecisionModal } from "@/components/DecisionModal";
import { useAuditStream } from "@/api/sse";
import GraphView from "@/views/GraphView";
import { DecisionView } from "@/views/DecisionView";
import { AuditView } from "@/views/AuditView";
import { QueueView } from "@/views/QueueView";
import { KanbanView } from "@/views/KanbanView";
import { Dashboard } from "@/views/Dashboard";
import { UserDrillDown } from "@/views/UserDrillDown";
import { HomeView } from "@/views/HomeView";
import { ActorsView } from "@/views/ActorsView";
import { SettingsView } from "@/views/SettingsView";
import "@/styles/globals.css";

function TreeView() {
  return <GraphView />;
}
function NotFoundView() {
  return <div>Not implemented: NotFoundView</div>;
}

function AppRoutes() {
  // Open audit SSE stream globally
  useAuditStream();

  return (
    <Layout>
      <GlobalDecisionModal />
      <ErrorBoundary>
      <Switch>
        <Route path="/" component={HomeView} />
        <Route path="/trees/:tree" component={TreeView} />
        <Route path="/trees/:tree/decisions/:id" component={DecisionView} />
        <Route path="/trees/:tree/queue/:kind" component={QueueView} />
        <Route path="/trees/:tree/audit" component={AuditView} />
        <Route path="/trees/:tree/kanban" component={KanbanView} />
        <Route path="/dashboard" component={Dashboard} />
        <Route path="/users/:handle" component={UserDrillDown} />
        <Route path="/actors" component={ActorsView} />
        <Route path="/settings" component={SettingsView} />
        <Route component={NotFoundView} />
      </Switch>
      </ErrorBoundary>
    </Layout>
  );
}

function GlobalDecisionModal() {
  const dm = useAppStore((s) => s.decisionModal);
  const close = useAppStore((s) => s.closeDecision);
  return (
    <DecisionModal
      tree={dm?.tree ?? ""}
      decisionId={dm?.id ?? null}
      isOpen={dm !== null}
      onClose={close}
    />
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

  // Set the class on <html> so HeroUI Modals (portaled to document.body)
  // and ReactFlow's CSS variables see it. Local div class isn't enough
  // because portals escape the React tree.
  useEffect(() => {
    const root = document.documentElement;
    root.classList.remove("light", "dark");
    root.classList.add(resolved);
    root.dataset.theme = resolved;
    root.style.colorScheme = resolved;
  }, [resolved]);

  return (
    <div className="text-foreground bg-background min-h-screen">{children}</div>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <HeroUIProvider>
        <ThemeWrapper>
          <Router base="/ui">
            <AppRoutes />
          </Router>
        </ThemeWrapper>
      </HeroUIProvider>
    </QueryClientProvider>
  );
}
