import { Link, useParams } from "wouter";
import { DecisionDetail } from "@/components/DecisionDetail";
import { ChevronLeft } from "lucide-react";
import { Button } from "@heroui/react";

export function DecisionView() {
  const params = useParams<{ tree: string; id: string }>();
  const tree = params.tree ?? "";
  const id = params.id ?? "";

  return (
    <div className="flex flex-col gap-4 p-4 max-w-4xl mx-auto">
      {/* Breadcrumb */}
      <nav className="flex items-center gap-2 text-sm text-default-500">
        <Link href="/">Home</Link>
        <span>/</span>
        <Link href={`/trees/${tree}`}>{tree}</Link>
        <span>/</span>
        <span className="text-default-800">Decision</span>
        <span className="font-mono text-default-400">{id}</span>
      </nav>

      {/* Back link */}
      <div>
        <Button
          as={Link}
          href={`/trees/${tree}`}
          variant="light"
          size="sm"
          startContent={<ChevronLeft size={16} />}
        >
          Back to tree
        </Button>
      </div>

      {/* Detail panel */}
      <DecisionDetail tree={tree} id={id} />
    </div>
  );
}
