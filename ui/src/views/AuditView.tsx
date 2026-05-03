import { useState, useRef, useCallback } from "react";
import {
  useReactTable,
  getCoreRowModel,
  flexRender,
  createColumnHelper,
} from "@tanstack/react-table";
import { Button, Input, Select, SelectItem, Chip, Tooltip, Spinner } from "@heroui/react";
import { useParams } from "wouter";
import { formatDistanceToNow } from "date-fns";
import { useAuditList } from "@/api/query";
import { useAuditStream } from "@/api/sse";
import type { Event as DtreeEvent, Action } from "@/api/types.gen";
// DtreeEvent is used in createColumnHelper generic and liveEvents state below

const ACTION_OPTIONS: Action[] = [
  "create", "update", "delete", "decide", "undecide", "scope_out",
  "supersede", "relate", "unrelate", "rename", "restore",
  "external_edit", "external_create", "external_delete",
  "tree_create", "tree_delete", "tree_rename", "tree_archive",
  "actor_add", "actor_rename", "actor_archive",
  "config_change", "schema_migrate",
];

const ACTION_COLOR_MAP: Record<string, "success" | "danger" | "warning" | "primary" | "default"> = {
  create: "success",
  external_create: "success",
  tree_create: "success",
  actor_add: "success",
  delete: "danger",
  external_delete: "danger",
  tree_delete: "danger",
  decide: "primary",
  undecide: "warning",
  scope_out: "warning",
  supersede: "warning",
  update: "default",
  external_edit: "default",
  relate: "default",
  unrelate: "default",
};

function ActionChip({ action }: { action: string }) {
  const color = ACTION_COLOR_MAP[action] ?? "default";
  return (
    <Chip size="sm" color={color} variant="flat">
      {action}
    </Chip>
  );
}

function RelativeTime({ ts }: { ts: string }) {
  const date = new Date(ts);
  const relative = formatDistanceToNow(date, { addSuffix: true });
  return (
    <Tooltip content={date.toLocaleString()}>
      <span className="cursor-default text-sm text-default-500">{relative}</span>
    </Tooltip>
  );
}

const columnHelper = createColumnHelper<DtreeEvent>();

const columns = [
  columnHelper.accessor("ts", {
    header: "Time",
    cell: (info) => <RelativeTime ts={info.getValue()} />,
  }),
  columnHelper.accessor("actor", {
    header: "Actor",
    cell: (info) => <span className="text-sm font-mono">{info.getValue()}</span>,
  }),
  columnHelper.accessor("action", {
    header: "Action",
    cell: (info) => <ActionChip action={info.getValue()} />,
  }),
  columnHelper.accessor("kind", {
    header: "Kind",
    cell: (info) => <span className="text-sm text-default-600">{info.getValue()}</span>,
  }),
  columnHelper.accessor("id", {
    header: "Ref",
    cell: (info) => (
      <span className="text-sm font-mono text-default-500">{info.getValue().slice(0, 8)}</span>
    ),
  }),
  columnHelper.accessor(
    (row) => {
      const after = row.payload?.after as Record<string, unknown> | undefined;
      return (after?.summary as string) ?? (after?.name as string) ?? row.id;
    },
    {
      id: "summary",
      header: "Summary",
      cell: (info) => (
        <span className="text-sm truncate max-w-xs block">{info.getValue()}</span>
      ),
    },
  ),
];

export function AuditView() {
  const params = useParams<{ tree: string }>();
  const tree = params.tree ?? "";

  const [actionFilter, setActionFilter] = useState("");
  const [actorFilter, setActorFilter] = useState("");
  const [sinceFilter, setSinceFilter] = useState("");
  const [liveTail, setLiveTail] = useState(false);
  const [liveEvents, setLiveEvents] = useState<DtreeEvent[]>([]);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [allItems, setAllItems] = useState<DtreeEvent[]>([]);
  const initialLoadDone = useRef(false);

  const filters = {
    ...(actionFilter ? { action: actionFilter } : {}),
    ...(actorFilter ? { actor: actorFilter } : {}),
    ...(sinceFilter ? { since: sinceFilter } : {}),
    ...(cursor ? { cursor } : {}),
  };

  const { data, isLoading, isFetching } = useAuditList(tree, filters);

  // Merge server data into allItems on first load or filter change
  const serverItems = data?.items ?? [];
  const nextCursor = data?.next_cursor;

  // When filters change, reset
  const handleFilterChange = useCallback(() => {
    setCursor(undefined);
    setAllItems([]);
    initialLoadDone.current = false;
  }, []);

  // Accumulate pages
  if (data && !isFetching) {
    if (!initialLoadDone.current && cursor === undefined) {
      initialLoadDone.current = true;
      if (allItems.length === 0) {
        setAllItems(serverItems);
      }
    }
  }

  // useAuditStream handles cache invalidation; liveEvents are populated via
  // query re-fetches triggered by the stream. For visual prepend we rely on
  // the global stream in App — this local stream just enables per-tree filtering.
  useAuditStream({ tree, enabled: liveTail });

  // Combine: live-tail events prepended, then allItems from server
  const displayItems = liveTail
    ? [...liveEvents, ...allItems]
    : allItems.length > 0
      ? allItems
      : serverItems;

  const table = useReactTable({
    data: displayItems,
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
  });

  const loadMore = () => {
    if (nextCursor) {
      setCursor(nextCursor);
      if (data?.items) {
        setAllItems((prev) => [...prev, ...(data.items)]);
      }
    }
  };

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Audit Log</h1>
        <Button
          size="sm"
          color={liveTail ? "success" : "default"}
          variant={liveTail ? "solid" : "bordered"}
          onPress={() => {
            if (!liveTail) setLiveEvents([]);
            setLiveTail((v) => !v);
          }}
        >
          {liveTail ? "Live tail ON" : "Live tail OFF"}
        </Button>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap gap-3 items-end">
        <Select
          label="Action"
          placeholder="All actions"
          size="sm"
          className="w-48"
          selectedKeys={actionFilter ? new Set([actionFilter]) : new Set()}
          onSelectionChange={(keys) => {
            const val = Array.from(keys)[0] as string ?? "";
            setActionFilter(val);
            handleFilterChange();
          }}
        >
          {ACTION_OPTIONS.map((a) => (
            <SelectItem key={a}>{a}</SelectItem>
          ))}
        </Select>
        <Input
          label="Actor"
          placeholder="Filter by actor"
          size="sm"
          className="w-48"
          value={actorFilter}
          onValueChange={(v) => {
            setActorFilter(v);
            handleFilterChange();
          }}
        />
        <Input
          label="Since"
          type="date"
          size="sm"
          className="w-48"
          value={sinceFilter}
          onValueChange={(v) => {
            setSinceFilter(v);
            handleFilterChange();
          }}
        />
        {(actionFilter || actorFilter || sinceFilter) && (
          <Button
            size="sm"
            variant="flat"
            onPress={() => {
              setActionFilter("");
              setActorFilter("");
              setSinceFilter("");
              handleFilterChange();
            }}
          >
            Clear filters
          </Button>
        )}
      </div>

      {/* Table */}
      {isLoading ? (
        <div className="flex justify-center py-12">
          <Spinner size="lg" />
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-divider">
          <table className="w-full text-left">
            <thead className="bg-default-100">
              {table.getHeaderGroups().map((hg) => (
                <tr key={hg.id}>
                  {hg.headers.map((header) => (
                    <th
                      key={header.id}
                      className="px-4 py-3 text-sm font-semibold text-default-600 whitespace-nowrap"
                    >
                      {flexRender(header.column.columnDef.header, header.getContext())}
                    </th>
                  ))}
                </tr>
              ))}
            </thead>
            <tbody>
              {table.getRowModel().rows.length === 0 ? (
                <tr>
                  <td colSpan={columns.length} className="px-4 py-8 text-center text-default-400">
                    No audit events found.
                  </td>
                </tr>
              ) : (
                table.getRowModel().rows.map((row) => (
                  <tr
                    key={row.id}
                    className="border-t border-divider hover:bg-default-50 transition-colors"
                  >
                    {row.getVisibleCells().map((cell) => (
                      <td key={cell.id} className="px-4 py-3">
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </td>
                    ))}
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}

      {/* Load more */}
      {nextCursor && !liveTail && (
        <div className="flex justify-center pt-2">
          <Button
            size="sm"
            variant="flat"
            isLoading={isFetching}
            onPress={loadMore}
          >
            Load more
          </Button>
        </div>
      )}
    </div>
  );
}
