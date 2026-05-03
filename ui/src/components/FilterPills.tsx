import { useEffect, useRef, useState } from "react";
import { Chip, Dropdown, DropdownTrigger, DropdownMenu, DropdownItem, Button, Input } from "@heroui/react";
import { Plus, X } from "lucide-react";
import { useSearch, useLocation } from "wouter";

export interface FilterDef {
  key: string;
  label: string;
  type: "enum" | "text";
  options?: string[];
}

export interface FilterPillsProps {
  filters: FilterDef[];
  values: Record<string, string | string[]>;
  onChange: (key: string, value: string | string[] | undefined) => void;
}

/**
 * useFilterParams — syncs filter state to/from URL query string.
 * Returns [values, setFilter, clearFilter].
 */
export function useFilterParams(filters: FilterDef[]): [
  Record<string, string | string[]>,
  (key: string, value: string | string[] | undefined) => void,
  (key: string) => void,
] {
  // wouter's useSearch returns the raw query string (no leading "?").
  // useLocation() returns the path RELATIVE to the Router base — pass it
  // back to navigate() unmodified to avoid double-prepending the base
  // (which produced /ui/ui/... earlier).
  const search = useSearch();
  const [location, navigate] = useLocation();
  const params = new URLSearchParams(search);

  const values: Record<string, string | string[]> = {};
  for (const f of filters) {
    const vals = params.getAll(f.key);
    if (vals.length === 0) continue;
    if (f.type === "enum") {
      values[f.key] = vals.length === 1 ? vals[0] : vals;
    } else {
      values[f.key] = vals[0];
    }
  }

  const setFilter = (key: string, value: string | string[] | undefined) => {
    const next = new URLSearchParams(params.toString());
    next.delete(key);
    if (value !== undefined) {
      if (Array.isArray(value)) {
        for (const v of value) next.append(key, v);
      } else {
        next.set(key, value);
      }
    }
    const qs = next.toString();
    navigate(location + (qs ? `?${qs}` : ""), { replace: true });
  };

  const clearFilter = (key: string) => setFilter(key, undefined);

  return [values, setFilter, clearFilter];
}

/** A single active filter rendered as a closeable Chip. */
function FilterChip({
  label,
  onRemove,
}: {
  label: string;
  onRemove: () => void;
}) {
  return (
    <Chip
      variant="flat"
      color="primary"
      endContent={
        <button
          aria-label={`Remove filter ${label}`}
          onClick={onRemove}
          className="ml-1 hover:text-danger"
        >
          <X size={12} />
        </button>
      }
    >
      {label}
    </Chip>
  );
}

/**
 * FilterPills — renders active filters as closeable chips plus a
 * Dropdown/Input to add new ones. State is controlled via props.
 */
export function FilterPills({ filters, values, onChange }: FilterPillsProps) {
  const activeKeys = Object.keys(values);

  // Text filter being edited inline
  const textFilters = filters.filter((f) => f.type === "text");

  // Filters not yet active (for the add dropdown)
  const inactive = filters.filter((f) => !(f.key in values));

  function chipLabel(f: FilterDef): string {
    const v = values[f.key];
    const display = Array.isArray(v) ? v.join(", ") : v;
    return `${f.label}: ${display}`;
  }

  return (
    <div className="flex flex-wrap items-center gap-2" data-testid="filter-pills">
      {/* Active filter chips */}
      {activeKeys.map((key) => {
        const def = filters.find((f) => f.key === key);
        if (!def) return null;
        return (
          <FilterChip
            key={key}
            label={chipLabel(def)}
            onRemove={() => onChange(key, undefined)}
          />
        );
      })}

      {/* Add filter dropdown for enum filters */}
      {inactive.filter((f) => f.type === "enum").length > 0 && (
        <Dropdown>
          <DropdownTrigger>
            <Button
              size="sm"
              variant="flat"
              startContent={<Plus size={14} />}
              aria-label="Add filter"
            >
              Filter
            </Button>
          </DropdownTrigger>
          <DropdownMenu aria-label="Add filter">
            {inactive
              .filter((f) => f.type === "enum")
              .flatMap((f) =>
                (f.options ?? []).map((opt) => (
                  <DropdownItem
                    key={`${f.key}:${opt}`}
                    onPress={() => {
                      const existing = values[f.key];
                      if (Array.isArray(existing)) {
                        onChange(f.key, [...existing, opt]);
                      } else if (existing) {
                        onChange(f.key, [existing, opt]);
                      } else {
                        onChange(f.key, opt);
                      }
                    }}
                  >
                    {f.label}: {opt}
                  </DropdownItem>
                )),
              )}
          </DropdownMenu>
        </Dropdown>
      )}

      {/* Text filter inputs — debounced */}
      {textFilters.map((f) => (
        <DebouncedTextFilter
          key={f.key}
          def={f}
          value={(values[f.key] as string | undefined) ?? ""}
          onChange={(v) => onChange(f.key, v || undefined)}
        />
      ))}
    </div>
  );
}

function DebouncedTextFilter({
  def,
  value,
  onChange,
}: {
  def: FilterDef;
  value: string;
  onChange: (v: string | undefined) => void;
}) {
  // Local state for the input; only push to URL after the user pauses typing.
  const [local, setLocal] = useState(value);
  const lastPushed = useRef(value);

  // Pull external changes (e.g. URL nav, clear button) back into the input.
  useEffect(() => {
    if (value !== lastPushed.current) {
      setLocal(value);
      lastPushed.current = value;
    }
  }, [value]);

  // Push local changes after 250ms of inactivity.
  useEffect(() => {
    if (local === lastPushed.current) return;
    const t = setTimeout(() => {
      lastPushed.current = local;
      onChange(local || undefined);
    }, 250);
    return () => clearTimeout(t);
  }, [local, onChange]);

  return (
    <Input
      size="sm"
      placeholder={`Filter ${def.label}`}
      value={local}
      onValueChange={setLocal}
      className="w-40"
      aria-label={`Filter by ${def.label}`}
      endContent={
        local ? (
          <button
            aria-label={`Clear ${def.label} filter`}
            onClick={() => {
              setLocal("");
              lastPushed.current = "";
              onChange(undefined);
            }}
          >
            <X size={12} />
          </button>
        ) : null
      }
    />
  );
}
