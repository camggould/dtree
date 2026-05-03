import {
  Button,
  Dropdown,
  DropdownTrigger,
  DropdownMenu,
  DropdownItem,
} from "@heroui/react";
import { ChevronDown } from "lucide-react";

/** Multi-select tree dropdown shared between Dashboard and per-user views.
 *  Empty selection means "all trees".
 */
export function TreeFilter({
  allSlugs,
  selected,
  setSelected,
}: {
  allSlugs: string[];
  selected: Set<string>;
  setSelected: (s: Set<string>) => void;
}) {
  const label =
    selected.size === 0 || selected.size === allSlugs.length
      ? "All trees"
      : `${selected.size} tree${selected.size === 1 ? "" : "s"}`;

  return (
    <Dropdown closeOnSelect={false}>
      <DropdownTrigger>
        <Button
          variant="flat"
          endContent={<ChevronDown size={14} />}
          size="sm"
        >
          Trees: {label}
        </Button>
      </DropdownTrigger>
      <DropdownMenu
        aria-label="Tree filter"
        selectionMode="multiple"
        selectedKeys={selected}
        onSelectionChange={(keys) => {
          if (keys === "all") setSelected(new Set(allSlugs));
          else setSelected(new Set(Array.from(keys, String)));
        }}
      >
        {allSlugs.map((slug) => (
          <DropdownItem key={slug}>{slug}</DropdownItem>
        ))}
      </DropdownMenu>
    </Dropdown>
  );
}
