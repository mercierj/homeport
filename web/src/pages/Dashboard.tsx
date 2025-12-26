import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { ContainerList } from '@/components/ContainerList';
import { LogViewer } from '@/components/LogViewer';
import type { Container } from '@/lib/docker-api';

export function Dashboard() {
  const [selectedContainer, setSelectedContainer] = useState<Container | null>(null);

  const { data: health, isLoading: healthLoading } = useQuery({
    queryKey: ['health'],
    queryFn: api.health,
  });

  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold">Dashboard</h1>

      <div className="grid gap-4 md:grid-cols-3">
        <div className="rounded-lg border p-4">
          <h3 className="font-medium text-muted-foreground">API Status</h3>
          <p className="text-2xl font-bold">
            {healthLoading ? 'Loading...' : health?.status || 'Unknown'}
          </p>
        </div>

        <div className="rounded-lg border p-4">
          <h3 className="font-medium text-muted-foreground">Stacks</h3>
          <p className="text-2xl font-bold">1</p>
        </div>

        <div className="rounded-lg border p-4">
          <h3 className="font-medium text-muted-foreground">Docker</h3>
          <p className="text-2xl font-bold text-green-600">Connected</p>
        </div>
      </div>

      <ContainerList
        stackId="default"
        onViewLogs={(container) => setSelectedContainer(container)}
      />

      {selectedContainer && (
        <LogViewer
          container={selectedContainer}
          onClose={() => setSelectedContainer(null)}
        />
      )}
    </div>
  );
}
