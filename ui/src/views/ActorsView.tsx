import { Link } from "wouter";
import {
  Card,
  CardBody,
  CardHeader,
  Spinner,
  Chip,
  Table,
  TableHeader,
  TableColumn,
  TableBody,
  TableRow,
  TableCell,
} from "@heroui/react";
import { useActors } from "@/api/query";

export function ActorsView() {
  const actorsQuery = useActors();

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <h1 className="text-2xl font-bold mb-4">Actors</h1>
      <p className="text-default-500 text-sm mb-6">
        People and agents registered in this project. Click any handle to drill
        into their decision activity.
      </p>

      <Card>
        <CardHeader>
          <h2 className="text-base font-semibold">
            {actorsQuery.data?.length ?? 0} registered
          </h2>
        </CardHeader>
        <CardBody>
          {actorsQuery.isLoading ? (
            <div className="flex justify-center py-8">
              <Spinner />
            </div>
          ) : (
            <Table aria-label="Actors" removeWrapper>
              <TableHeader>
                <TableColumn>Handle</TableColumn>
                <TableColumn>Name</TableColumn>
                <TableColumn>Email</TableColumn>
                <TableColumn>Kind</TableColumn>
                <TableColumn>Status</TableColumn>
              </TableHeader>
              <TableBody>
                {(actorsQuery.data ?? []).map((a) => (
                  <TableRow key={a.handle}>
                    <TableCell>
                      <Link href={`/users/${a.handle}`}>
                        <span className="text-primary hover:underline cursor-pointer">
                          {a.handle}
                        </span>
                      </Link>
                    </TableCell>
                    <TableCell>{a.name ?? a.display_name}</TableCell>
                    <TableCell className="text-default-500">
                      {a.email ?? ""}
                    </TableCell>
                    <TableCell>
                      <Chip
                        size="sm"
                        variant="flat"
                        color={a.kind === "agent" ? "secondary" : "primary"}
                      >
                        {a.kind}
                      </Chip>
                    </TableCell>
                    <TableCell>
                      <Chip
                        size="sm"
                        variant="flat"
                        color={a.active ? "success" : "default"}
                      >
                        {a.active ? "Active" : "Archived"}
                      </Chip>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardBody>
      </Card>
    </div>
  );
}

export default ActorsView;
