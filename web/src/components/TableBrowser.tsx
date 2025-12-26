import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { listTables, getTableData } from '@/lib/database-api';
import { cn } from '@/lib/utils';
import { Table as TableIcon } from 'lucide-react';

interface Props {
  stackId?: string;
}

export function TableBrowser({ stackId = 'default' }: Props) {
  const [selectedTable, setSelectedTable] = useState<string | null>(null);

  const tablesQuery = useQuery({
    queryKey: ['tables', stackId],
    queryFn: () => listTables(stackId),
  });

  const dataQuery = useQuery({
    queryKey: ['table-data', stackId, selectedTable],
    queryFn: () => getTableData(stackId, selectedTable!, 'public', 50),
    enabled: !!selectedTable,
  });

  return (
    <div className="grid grid-cols-4 gap-4">
      {/* Table List */}
      <div className="border rounded-lg p-4">
        <h3 className="font-medium mb-2">Tables</h3>
        {tablesQuery.isLoading ? (
          <p className="text-sm text-muted-foreground">Loading...</p>
        ) : (
          <ul className="space-y-1">
            {tablesQuery.data?.tables.map((table) => (
              <li key={table.name}>
                <button
                  onClick={() => setSelectedTable(table.name)}
                  className={cn(
                    "w-full flex items-center gap-2 px-2 py-1 rounded text-sm text-left",
                    selectedTable === table.name
                      ? "bg-primary text-primary-foreground"
                      : "hover:bg-muted"
                  )}
                >
                  <TableIcon className="h-4 w-4" />
                  {table.name}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      {/* Table Data */}
      <div className="col-span-3 border rounded-lg p-4">
        {selectedTable ? (
          <>
            <h3 className="font-medium mb-4">{selectedTable}</h3>
            {dataQuery.isLoading ? (
              <p className="text-muted-foreground">Loading...</p>
            ) : dataQuery.data ? (
              <div className="overflow-auto">
                <table className="w-full text-sm">
                  <thead className="bg-muted">
                    <tr>
                      {dataQuery.data.columns.map((col) => (
                        <th key={col} className="px-4 py-2 text-left font-medium">
                          {col}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {dataQuery.data.rows.map((row, i) => (
                      <tr key={i} className="border-t">
                        {row.map((cell, j) => (
                          <td key={j} className="px-4 py-2 max-w-xs truncate">
                            {cell === null ? (
                              <span className="text-muted-foreground">NULL</span>
                            ) : (
                              String(cell)
                            )}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
          </>
        ) : (
          <p className="text-muted-foreground">Select a table to view data</p>
        )}
      </div>
    </div>
  );
}
