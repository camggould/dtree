import { useEffect, useRef } from "react";
import { keys, queryClient } from "@/api/query";
import type { Event as DtreeEvent } from "@/api/types.gen";

interface UseAuditStreamOptions {
  tree?: string;
  enabled?: boolean;
}

/**
 * Opens a Server-Sent Events connection to /v1/audit/stream.
 * On each event, invalidates relevant TanStack Query keys.
 * Auto-reconnects with exponential back-off on error/close.
 */
export function useAuditStream({
  tree,
  enabled = true,
}: UseAuditStreamOptions = {}): void {
  const esRef = useRef<EventSource | null>(null);
  const retryTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);
  const retryDelay = useRef(1000);

  useEffect(() => {
    if (!enabled) return;

    let cancelled = false;

    function connect() {
      if (cancelled) return;

      const url = tree
        ? `/v1/audit/stream?tree=${encodeURIComponent(tree)}`
        : "/v1/audit/stream";

      const es = new EventSource(url);
      esRef.current = es;

      es.onopen = () => {
        retryDelay.current = 1000; // reset back-off
      };

      es.onmessage = (e: MessageEvent) => {
        try {
          const event = JSON.parse(e.data as string) as DtreeEvent;
          invalidateForEvent(event);
        } catch {
          // ignore malformed events
        }
      };

      es.onerror = () => {
        es.close();
        esRef.current = null;
        if (!cancelled) {
          const delay = retryDelay.current;
          retryDelay.current = Math.min(delay * 2, 30_000);
          retryTimeout.current = setTimeout(connect, delay);
        }
      };
    }

    connect();

    return () => {
      cancelled = true;
      if (retryTimeout.current) {
        clearTimeout(retryTimeout.current);
        retryTimeout.current = null;
      }
      esRef.current?.close();
      esRef.current = null;
    };
  }, [tree, enabled]);
}

function invalidateForEvent(event: DtreeEvent): void {
  const { kind, tree: eventTree, action } = event;

  // Always invalidate audit log
  void queryClient.invalidateQueries({ queryKey: keys.audit(eventTree) });
  void queryClient.invalidateQueries({ queryKey: keys.audit() });

  if (kind === "decision" && eventTree) {
    void queryClient.invalidateQueries({
      queryKey: keys.decisions(eventTree),
    });
    void queryClient.invalidateQueries({
      queryKey: keys.decision(eventTree, event.id),
    });
    void queryClient.invalidateQueries({
      queryKey: keys.metrics(eventTree),
    });
  }

  if (kind === "tree") {
    void queryClient.invalidateQueries({ queryKey: keys.trees() });
    if (eventTree) {
      void queryClient.invalidateQueries({ queryKey: keys.tree(eventTree) });
    }
  }

  if (kind === "actor") {
    void queryClient.invalidateQueries({ queryKey: keys.actors() });
  }

  // On tree_delete / tree_archive we also want to refresh the tree list
  if (
    action === "tree_delete" ||
    action === "tree_archive" ||
    action === "tree_create"
  ) {
    void queryClient.invalidateQueries({ queryKey: keys.trees() });
  }
}
