import { useState } from "react";
import { useLocation, Link } from "wouter";
import {
  Navbar,
  NavbarBrand,
  NavbarContent,
  NavbarItem,
  Button,
  Dropdown,
  DropdownTrigger,
  DropdownMenu,
  DropdownItem,
  Tabs,
  Tab,
} from "@heroui/react";
import {
  TreeDeciduous,
  LayoutDashboard,
  Users,
  Settings,
  Sun,
  Moon,
  Monitor,
  ChevronDown,
  PanelLeftClose,
  PanelLeft,
  Clock,
} from "lucide-react";
import { IdentitySelector } from "@/components/IdentitySelector";
import { useAppStore } from "@/store/app";
import { useTrees } from "@/api/query";
import { ROUTES } from "@/routes";

interface LayoutProps {
  children: React.ReactNode;
}

export function Layout({ children }: LayoutProps) {
  const [location] = useLocation();
  const { theme, setTheme, lastTreeSlug } = useAppStore();
  const { data: trees } = useTrees();
  const [sidebarOpen, setSidebarOpen] = useState(true);

  // Determine current tree from URL
  const treeMatch = location.match(/^\/trees\/([^/]+)/);
  const currentTree = treeMatch ? treeMatch[1] : null;

  const themeIcon =
    theme === "light" ? (
      <Sun size={16} />
    ) : theme === "dark" ? (
      <Moon size={16} />
    ) : (
      <Monitor size={16} />
    );

  const cycleTheme = () => {
    if (theme === "system") setTheme("light");
    else if (theme === "light") setTheme("dark");
    else setTheme("system");
  };

  // View tabs for tree context
  const treeForTabs = currentTree ?? lastTreeSlug;
  const viewTabs = treeForTabs
    ? [
        { key: "tree", label: "Overview", href: ROUTES.tree(treeForTabs) },
        {
          key: "decisions",
          href: ROUTES.tree(treeForTabs),
          label: "Decisions",
        },
        {
          key: "audit",
          label: "Audit",
          href: ROUTES.audit(treeForTabs),
        },
      ]
    : [];

  return (
    <div className="min-h-screen flex flex-col">
      <Navbar isBordered maxWidth="full">
        <NavbarBrand>
          <Link href="/" className="flex items-center gap-2 font-bold text-lg">
            <TreeDeciduous size={20} className="text-primary" />
            dtree
          </Link>
        </NavbarBrand>

        <NavbarContent className="hidden sm:flex gap-2" justify="center">
          {/* Tree dropdown */}
          <NavbarItem>
            <Dropdown>
              <DropdownTrigger>
                <Button
                  variant="light"
                  endContent={<ChevronDown size={14} />}
                  size="sm"
                >
                  {currentTree ?? "Select tree"}
                </Button>
              </DropdownTrigger>
              <DropdownMenu aria-label="Trees">
                {trees && trees.length > 0
                  ? trees.map((t) => (
                      <DropdownItem key={t.slug}>
                        <Link href={ROUTES.tree(t.slug)}>
                          {t.name ?? t.slug}
                        </Link>
                      </DropdownItem>
                    ))
                  : [
                      <DropdownItem key="empty" isDisabled>
                        No trees
                      </DropdownItem>,
                    ]}
              </DropdownMenu>
            </Dropdown>
          </NavbarItem>

          {/* View tabs */}
          {viewTabs.length > 0 && (
            <NavbarItem>
              <Tabs size="sm" selectedKey={location} aria-label="View tabs">
                {viewTabs.map((tab) => (
                  <Tab
                    key={tab.href}
                    title={
                      <Link href={tab.href} className="no-underline">
                        {tab.label}
                      </Link>
                    }
                  >
                    {null}
                  </Tab>
                ))}
              </Tabs>
            </NavbarItem>
          )}
        </NavbarContent>

        <NavbarContent justify="end" className="gap-1">
          <NavbarItem>
            <Button
              as={Link}
              href={ROUTES.dashboard}
              variant="light"
              size="sm"
              startContent={<LayoutDashboard size={16} />}
            >
              Dashboard
            </Button>
          </NavbarItem>
          <NavbarItem>
            <Button
              as={Link}
              href={ROUTES.actors}
              variant="light"
              size="sm"
              startContent={<Users size={16} />}
            >
              Actors
            </Button>
          </NavbarItem>
          <NavbarItem>
            <IdentitySelector />
          </NavbarItem>
          <NavbarItem>
            <Button
              isIconOnly
              variant="light"
              size="sm"
              onPress={cycleTheme}
              aria-label={`Theme: ${theme}`}
            >
              {themeIcon}
            </Button>
          </NavbarItem>
          <NavbarItem>
            <Button
              as={Link}
              href={ROUTES.settings}
              isIconOnly
              variant="light"
              size="sm"
              aria-label="Settings"
            >
              <Settings size={16} />
            </Button>
          </NavbarItem>
        </NavbarContent>
      </Navbar>

      <div className="flex flex-1 overflow-hidden">
        {/* Collapsible sidebar */}
        <aside
          className={`
            border-r border-divider flex flex-col transition-all duration-200
            ${sidebarOpen ? "w-56" : "w-0 overflow-hidden"}
          `}
        >
          <div className="p-3 flex items-center justify-between border-b border-divider">
            <span className="text-xs font-semibold uppercase text-default-500 tracking-wider">
              Recent Decisions
            </span>
          </div>
          <div className="flex-1 overflow-y-auto p-2">
            <div className="flex flex-col gap-1">
              <div className="flex items-center gap-2 text-xs text-default-400 p-2">
                <Clock size={12} />
                <span>No recent decisions</span>
              </div>
            </div>
          </div>
        </aside>

        {/* Sidebar toggle */}
        <button
          onClick={() => setSidebarOpen((v) => !v)}
          className="absolute left-0 top-1/2 -translate-y-1/2 z-10 bg-content1 border border-divider rounded-r p-1 text-default-400 hover:text-default-600"
          aria-label={sidebarOpen ? "Collapse sidebar" : "Expand sidebar"}
          style={{ marginLeft: sidebarOpen ? "224px" : "0" }}
        >
          {sidebarOpen ? <PanelLeftClose size={14} /> : <PanelLeft size={14} />}
        </button>

        {/* Main content */}
        <main className="flex-1 overflow-auto p-6">{children}</main>
      </div>
    </div>
  );
}
