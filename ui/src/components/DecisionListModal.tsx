import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  Chip,
  Button,
} from "@heroui/react";
import { Link } from "wouter";
import type { Decision } from "@/api/types.gen";
import { humanStatus, statusColor } from "@/util/labels";

/** Reusable modal that lists decisions filtered by some metric/segment.
 *  Click a row → navigate to its detail panel.
 */
export function DecisionListModal({
  isOpen,
  onClose,
  title,
  description,
  decisions,
}: {
  isOpen: boolean;
  onClose: () => void;
  title: string;
  description?: string;
  decisions: Decision[];
}) {
  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      size="3xl"
      scrollBehavior="inside"
    >
      <ModalContent>
        <ModalHeader className="flex flex-col gap-1">
          <h2 className="text-lg font-semibold">{title}</h2>
          {description && (
            <p className="text-sm text-default-500 font-normal">
              {description}
            </p>
          )}
          <p className="text-xs text-default-400">
            {decisions.length} decision{decisions.length === 1 ? "" : "s"}
          </p>
        </ModalHeader>
        <ModalBody className="pb-6">
          {decisions.length === 0 ? (
            <p className="text-default-400 text-sm py-6 text-center">
              Nothing to show
            </p>
          ) : (
            <div className="flex flex-col gap-2">
              {decisions.map((d) => (
                <Link
                  key={d.id}
                  href={`/trees/${d.tree}/decisions/${d.id}`}
                  onClick={onClose}
                >
                  <div className="border border-default-200 hover:border-primary rounded-md p-3 cursor-pointer transition-colors">
                    <div className="flex items-start justify-between gap-3">
                      <div className="flex-1 min-w-0">
                        <div className="font-medium text-sm truncate">
                          {d.summary}
                        </div>
                        <div className="text-xs text-default-500 mt-1 flex gap-2 flex-wrap">
                          <span>{d.tree}</span>
                          <span>·</span>
                          <span>by {d.creator}</span>
                          {d.recommended_by && (
                            <>
                              <span>·</span>
                              <span>rec: {d.recommended_by}</span>
                            </>
                          )}
                        </div>
                      </div>
                      <div className="flex flex-col items-end gap-1 shrink-0">
                        <Chip
                          size="sm"
                          variant="flat"
                          color={statusColor(d.status)}
                        >
                          {humanStatus(d.status)}
                        </Chip>
                        <span className="text-xs text-default-400 font-mono">
                          {d.priority}
                        </span>
                      </div>
                    </div>
                  </div>
                </Link>
              ))}
            </div>
          )}
          <div className="flex justify-end pt-2">
            <Button size="sm" variant="light" onPress={onClose}>
              Close
            </Button>
          </div>
        </ModalBody>
      </ModalContent>
    </Modal>
  );
}
