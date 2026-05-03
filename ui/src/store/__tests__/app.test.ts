import { describe, it, expect, beforeEach } from "vitest";
import { useAppStore } from "@/store/app";

describe("useAppStore", () => {
  beforeEach(() => {
    // Reset store state between tests
    useAppStore.setState({
      currentHandle: null,
      theme: "system",
      lastTreeSlug: null,
    });
  });

  it("setHandle persists the handle", () => {
    const { setHandle } = useAppStore.getState();
    setHandle("alice");

    const { currentHandle } = useAppStore.getState();
    expect(currentHandle).toBe("alice");
  });

  it("setHandle can clear the handle", () => {
    useAppStore.setState({ currentHandle: "bob" });
    useAppStore.getState().setHandle(null);
    expect(useAppStore.getState().currentHandle).toBeNull();
  });

  it("setTheme updates theme", () => {
    useAppStore.getState().setTheme("dark");
    expect(useAppStore.getState().theme).toBe("dark");
  });
});
