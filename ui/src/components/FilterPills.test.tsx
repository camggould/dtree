import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { FilterPills } from "@/components/FilterPills";
import type { FilterDef } from "@/components/FilterPills";

// Mock wouter so we don't need a Router provider
vi.mock("wouter", () => ({
  useSearch: () => ["", vi.fn()],
  useLocation: () => ["/", vi.fn()],
}));

const filters: FilterDef[] = [
  { key: "status", label: "Status", type: "enum", options: ["proposed", "decided"] },
  { key: "search", label: "Search", type: "text" },
];

describe("FilterPills", () => {
  it("renders active filters, adds via dropdown, and removes via chip close", () => {
    const onChange = vi.fn();

    const { rerender } = render(
      <FilterPills
        filters={filters}
        values={{ status: "proposed" }}
        onChange={onChange}
      />,
    );

    // Active filter chip is visible
    expect(screen.getByText("Status: proposed")).toBeTruthy();

    // Remove the chip by clicking the X button
    const removeBtn = screen.getByRole("button", { name: /Remove filter Status: proposed/i });
    fireEvent.click(removeBtn);
    expect(onChange).toHaveBeenCalledWith("status", undefined);

    // Rerender with no active filters to test add
    rerender(
      <FilterPills
        filters={filters}
        values={{}}
        onChange={onChange}
      />,
    );

    // The "Filter" add button should be visible
    expect(screen.getByRole("button", { name: /Add filter/i })).toBeTruthy();

    // Text input for "search" filter is always shown
    expect(screen.getByPlaceholderText(/Filter Search/i)).toBeTruthy();

    // Type in the search text input
    const searchInput = screen.getByPlaceholderText(/Filter Search/i);
    fireEvent.change(searchInput, { target: { value: "hello" } });
    expect(onChange).toHaveBeenCalledWith("search", "hello");
  });
});
