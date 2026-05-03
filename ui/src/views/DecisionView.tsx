import { useEffect, useState } from "react";
import { useParams } from "wouter";
import GraphView from "@/views/GraphView";
import { DecisionModal } from "@/components/DecisionModal";

/**
 * Deep-link route for a single decision: `/trees/:tree/decisions/:id`.
 * Renders the tree graph behind a decision modal, so closing the modal
 * lands the user on the same tree they linked from. Honors browser-back
 * via window.history (drops the /decisions/:id segment).
 */
export function DecisionView() {
  const params = useParams<{ tree: string; id: string }>();
  const tree = params.tree ?? "";
  const id = params.id ?? "";

  const [open, setOpen] = useState(true);

  useEffect(() => setOpen(true), [id]);

  const close = () => {
    setOpen(false);
    // Pop the /decisions/:id segment without a full reload.
    if (window.history.length > 1) {
      window.history.back();
    } else {
      window.location.assign(`/ui/trees/${tree}`);
    }
  };

  return (
    <>
      <GraphView />
      <DecisionModal tree={tree} decisionId={id} isOpen={open} onClose={close} />
    </>
  );
}

export default DecisionView;
