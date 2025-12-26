import { useState } from 'react';
import Editor from '@monaco-editor/react';
import { useMutation } from '@tanstack/react-query';
import { executeQuery } from '@/lib/database-api';
import type { QueryResult } from '@/lib/database-api';
import { Button } from '@/components/ui/button';
import { Play, Loader2 } from 'lucide-react';

interface Props {
  stackId?: string;
}

export function QueryEditor({ stackId = 'default' }: Props) {
  const [query, setQuery] = useState('SELECT version();');
  const [result, setResult] = useState<QueryResult | null>(null);

  const mutation = useMutation({
    mutationFn: () => executeQuery(stackId, query),
    onSuccess: (data) => setResult(data),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h3 className="font-semibold">Query Editor</h3>
        <Button
          size="sm"
          onClick={() => mutation.mutate()}
          disabled={mutation.isPending}
        >
          {mutation.isPending ? (
            <Loader2 className="h-4 w-4 mr-2 animate-spin" />
          ) : (
            <Play className="h-4 w-4 mr-2" />
          )}
          Run Query
        </Button>
      </div>

      <div className="border rounded-md overflow-hidden">
        <Editor
          height="150px"
          defaultLanguage="sql"
          value={query}
          onChange={(value) => setQuery(value || '')}
          options={{
            minimap: { enabled: false },
            fontSize: 14,
            lineNumbers: 'off',
            scrollBeyondLastLine: false,
          }}
        />
      </div>

      {mutation.isError && (
        <div className="p-4 bg-red-50 border border-red-200 rounded-md text-red-700 text-sm">
          {mutation.error.message}
        </div>
      )}

      {result && (
        <div className="border rounded-md overflow-auto">
          <table className="w-full text-sm">
            <thead className="bg-muted">
              <tr>
                {result.columns.map((col) => (
                  <th key={col} className="px-4 py-2 text-left font-medium">
                    {col}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {result.rows.map((row, i) => (
                <tr key={i} className="border-t">
                  {row.map((cell, j) => (
                    <td key={j} className="px-4 py-2">
                      {cell === null ? <span className="text-muted-foreground">NULL</span> : String(cell)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
          <div className="px-4 py-2 bg-muted text-sm text-muted-foreground">
            {result.row_count} rows
          </div>
        </div>
      )}
    </div>
  );
}
