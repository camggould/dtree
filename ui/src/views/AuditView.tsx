import { useState, useRef, useCallback, useMemo } from "react";
import {
  useReactTable,
  getCoreRowModel,
  flexRender,
  createColumnHelper,
} from "@tanstack/react-table";
import {
  Button,
  Input,
  Select,
  SelectItem,
  Chip,
  Tooltip,
  Spinner,
} from "@heroui/react";
import { useParams } from "wouter";
import { formatDistanceToNow } from "date-fns";
import { useAuditList, useActors } from "@/api/query";
import { useAuditStream } from "@/api/sse";
import { useAppStore } from "@/store/app";
import { humanAction } from "@/util/labels";
import type { Event as DtreeEvent, Action, Actor } from "@/api/types.gen";

const ACTION_OPTIONS: Action[] = [
  "create", "update", "delete", "decide", "undecide", "scope_out",
  "supersede", "relate", "unrelate", "rename", "restore",
  "external_edit", "external_create", "external_delete",
  "tree_create", "tree_delete", "tree_rename", "tree_archive",
  "actor_add", "actor_rename", "actor_archive",
  "config_change", "schema_migrate",
];

const ACTION_COLOR_MAP: Record<
  string,
  "success" | "danger" | "warning" | "primary" | "default"
> = {
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
  restore: "success",
  update: "default",
  external_edit: "default",
  relate: "default",
  unrelate: "default",
};

function ActionChip({ action }: { action: string }) {
  const color = ACTION_COLOR_MAP[action] ?? "default";
  return (
    <Chip size="sm" color={color} variant="flat">
      {humanAction(action)}
    </Chip>
  );
}

function ActorCell({
  actor,
  actors,
}: {
  actor: string;
  actors: Actor[] | undefined;
}) {
  const a = actors?.find((x) => x.handle === actor);
  return (
    <div className="flex items-center gap-1.5">
      <span className="text-sm font-medium">{actor}</span>
      {a && (
        <Chip
          size="sm"
          variant="flat"
          color={a.kind === "agent" ? "secondary" : "primary"}
          className="h-4 text-[10px]"
        >
          {a.kind}
        </Chip>
      )}
    </div>
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

function buildColumns(actors: Actor[] | undefined) {
  return [
    columnHelper.accessor("ts", {
      header: "Time",
      cell: (info) => <RelativeTime ts={info.getValue()} />,
    }),
    columnHelper.accessor("actor", {
      header: "Actor",
      cell: (info) => <ActorCell actor={info.getValue()} actors={actors} />,
    }),
    columnHelper.accessor("action", {
      header: "Action",
      cell: (info) => <ActionChip action={info.getValue()} />,
    }),
    columnHelper.accessor("kind", {
      header: "Kind",
      cell: (info) => (
        <span className="text-sm text-default-600">{info.getValue()}</span>
      ),
    }),
    columnHelper.accessor("id", {
      header: "Ref",
      cell: (info) => (
        <span className="text-sm font-mono text-default-500">
          {info.getValue().slice(0, 8)}
        </span>
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
          <span className="text-sm truncate max-w-xs block">
            {info.getValue()}
          </span>
        ),
      },
    ),
  ];
}

export function AuditView() {
  const params = useParams<{ tree: string }>();
  const tree = params.tree ?? "";
  const openDecision = useAppStore((s) => s.openDecision);
  const actorsQuery = useActors();

  const [actionFilter, setActionFilter] = useState("");
  const [actorFilter, setActorFilter] = useState("");
  const [sinceFilter, setSinceFilter] = useState("");
  const [liveTail, setLiveTail] = useState(false);
  const [liveEvents] = useState<DtreeEvent[]>([]);
  const [cursor, setCursor] = useState<string | undefined>(undefined);
  const [allItems, setAllItems] = useState<DtreeEvent[]>([]);
  const initialLoadDone = useRef(false);

  const sinceISO =
    sinceFilter && sinceFilter.length === 10
      ? `${sinceFilter}T00:00:00Z`
      : sinceFilter;

  const filters = {
    ...(actionFilter ? { action: actionFilter } : {}),
    ...(actorFilter ? { actor: actorFilter } : {}),
    ...(sinceISO ? { since: sinceISO } : {}),
    ...(cursor ? { cursor } : {}),
  };

  const { data, isLoading, isFetching } = useAuditList(tree, filters);

  const serverItems = data?.items ?? [];
  const nextCursor = data?.next_cursor;

  const handleFilterChange = useCallback(() => {
    setCursor(undefined);
    setAllItems([]);
    initialLoadDone.current = false;
  }, []);

  if (data && !isFetching) {
    if (!initialLoadDone.current && cursor === undefined) {
      initialLoadDone.current = true;
      if (allItems.length === 0) setAllItems(serverItems);
    }
  }

  useAuditStream({ tree, enabled: liveTail });

  const displayItems = liveTail
    ? [...liveEvents, ...allItems]
    : allItems.length > 0
      ? allItems
      : serverItems;

  const columns = useMemo(() => buildColumns(actorsQuery.data), [actorsQuery.data]);

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
        setAllItems((prev) => [...prev, ...data.items]);
      }
    }
  };

  return (
    <div className="p-6 space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Audit log</h1>
        <Button
          size="sm"
          color={liveTail ? "success" : "default"}
          variant={liveTail ? "solid" : "bordered"}
          onPress={() => setLiveTail((v) => !v)}
        >
          {liveTail ? "Live tail ON" : "Live tail OFF"}
        </Button>
      </div>

      <div className="flex flex-wrap gap-3 items-end">
        <Select
          label="Action"
          placeholder="All actions"
          size="sm"
          className="w-56"
          selectedKeys={actionFilter ? new Set([actionFilter]) : new Set()}
          onSelectionChange={(keys) => {
            const val = (Array.from(keys)[0] as string) ?? "";
            setActionFilter(val);
            handleFilterChange();
          }}
        >
          {ACTION_OPTIONS.map((a) => (
            <SelectItem key={a}>{humanAction(a)}</SelectItem>
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
                      {flexRender(
                        header.column.columnDef.header,
                        header.getContext(),
                      )}
                    </th>
                  ))}
                </tr>
              ))}
            </thead>
            <tbody>
              {table.getRowModel().rows.length === 0 ? (
                <tr>
                  <td
                    colSpan={columns.length}
                    className="px-4 py-8 text-center text-default-400"
                  >
                    No audit events found.
                  </td>
                </tr>
              ) : (
                table.getRowModel().rows.map((row) => {
                  const ev = row.original;
                  const clickable =
                    ev.kind === "decision" && Boolean(ev.tree) && Boolean(ev.id);
                  return (
                    <tr
                      key={row.id}
                      onClick={() => {
                        if (clickable) openDecision(ev.tree as string, ev.id);
                      }}
                      className={`border-t border-divider transition-colors ${
                        clickable
                          ? "hover:bg-default-100 cursor-pointer"
                          : ""
                      }`}
                    >
                      {row.getVisibleCells().map((cell) => (
                        <td key={cell.id} className="px-4 py-3">
                          {flexRender(
                            cell.column.columnDef.cell,
                            cell.getContext(),
                          )}
                        </td>
                      ))}
                    </tr>
                  );
                })
              )}
            </tbody>
          </table>
        </div>
      )}

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
