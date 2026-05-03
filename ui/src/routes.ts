export const ROUTES = {
  home: "/",
  dashboard: "/dashboard",
  actors: "/actors",
  settings: "/settings",
  tree: (tree: string) => `/trees/${tree}`,
  decision: (tree: string, id: string) => `/trees/${tree}/decisions/${id}`,
  queue: (tree: string, kind: string) => `/trees/${tree}/queue/${kind}`,
  audit: (tree: string) => `/trees/${tree}/audit`,
} as const;
