import { useState } from "react";
import {
  Dropdown,
  DropdownTrigger,
  DropdownMenu,
  DropdownItem,
  Button,
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
  Input,
  useDisclosure,
} from "@heroui/react";
import { User } from "lucide-react";
import { useAppStore } from "@/store/app";

export function IdentitySelector() {
  const { currentHandle, setHandle } = useAppStore();
  const { isOpen, onOpen, onClose } = useDisclosure();
  const [draftHandle, setDraftHandle] = useState("");

  // Show first-run modal when handle is null
  const showFirstRun = currentHandle === null;

  function handleConfirm() {
    const trimmed = draftHandle.trim();
    if (trimmed) {
      setHandle(trimmed);
      onClose();
    }
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Enter") handleConfirm();
  }

  return (
    <>
      {/* First-run modal */}
      <Modal
        isOpen={showFirstRun || isOpen}
        onClose={currentHandle !== null ? onClose : undefined}
        isDismissable={currentHandle !== null}
        hideCloseButton={currentHandle === null}
      >
        <ModalContent>
          <ModalHeader>Set your identity</ModalHeader>
          <ModalBody>
            <p className="text-sm text-default-500 mb-2">
              Enter your handle to identify yourself in decisions.
            </p>
            <Input
              autoFocus
              label="Handle"
              placeholder="e.g. alice"
              value={draftHandle}
              onValueChange={setDraftHandle}
              onKeyDown={handleKeyDown}
            />
          </ModalBody>
          <ModalFooter>
            {currentHandle !== null && (
              <Button variant="light" onPress={onClose}>
                Cancel
              </Button>
            )}
            <Button
              color="primary"
              onPress={handleConfirm}
              isDisabled={!draftHandle.trim()}
            >
              Confirm
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* Navbar dropdown (shown once handle is set) */}
      {currentHandle !== null && (
        <Dropdown>
          <DropdownTrigger>
            <Button
              variant="light"
              startContent={<User size={16} />}
              size="sm"
            >
              {currentHandle}
            </Button>
          </DropdownTrigger>
          <DropdownMenu aria-label="Identity options">
            <DropdownItem key="change" onPress={onOpen}>
              Change handle
            </DropdownItem>
            <DropdownItem
              key="clear"
              className="text-danger"
              color="danger"
              onPress={() => setHandle(null)}
            >
              Clear identity
            </DropdownItem>
          </DropdownMenu>
        </Dropdown>
      )}
    </>
  );
}
