import { useEffect, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import type { Container } from '@/lib/docker-api';
import { getContainerLogs } from '@/lib/docker-api';
import { Button } from '@/components/ui/button';
import { X, RefreshCw } from 'lucide-react';

interface LogViewerProps {
  container: Container;
  stackId?: string;
  onClose: () => void;
}

export function LogViewer({ container, stackId = 'default', onClose }: LogViewerProps) {
  const logRef = useRef<HTMLPreElement>(null);

  const { data, isLoading, refetch } = useQuery({
    queryKey: ['logs', stackId, container.name],
    queryFn: () => getContainerLogs(stackId, container.name, 200),
    refetchInterval: 2000,
  });

  useEffect(() => {
    if (logRef.current) {
      logRef.current.scrollTop = logRef.current.scrollHeight;
    }
  }, [data?.logs]);

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-background rounded-lg shadow-xl w-full max-w-4xl max-h-[80vh] flex flex-col">
        <div className="flex items-center justify-between p-4 border-b">
          <div>
            <h3 className="font-semibold">{container.name}</h3>
            <p className="text-sm text-muted-foreground">Container Logs</p>
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              <RefreshCw className="h-4 w-4" />
            </Button>
            <Button variant="ghost" size="sm" onClick={onClose}>
              <X className="h-4 w-4" />
            </Button>
          </div>
        </div>

        <pre
          ref={logRef}
          className="flex-1 overflow-auto p-4 bg-muted/50 text-sm font-mono"
        >
          {isLoading ? 'Loading logs...' : data?.logs || 'No logs available'}
        </pre>
      </div>
    </div>
  );
}
