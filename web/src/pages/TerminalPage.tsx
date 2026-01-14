import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Terminal } from '../components/Terminal';
import { listContainers, type Container } from '../lib/docker-api';

export function TerminalPage() {
  const [selectedContainer, setSelectedContainer] = useState<Container | null>(null);
  const stackId = 'default';

  const { data, isLoading } = useQuery({
    queryKey: ['containers', stackId],
    queryFn: () => listContainers(stackId),
  });

  const containers = data?.containers || [];
  const runningContainers = containers.filter((c: Container) => c.status === 'running');

  return (
    <div className="space-y-6 h-[calc(100vh-12rem)]">
      <div>
        <h1 className="text-2xl font-bold">Terminal</h1>
        <p className="text-muted-foreground">Access container shell</p>
      </div>

      {!selectedContainer ? (
        <div className="border rounded-lg p-6">
          <h2 className="text-lg font-semibold mb-4">Select a container</h2>
          {isLoading ? (
            <p className="text-muted-foreground">Loading containers...</p>
          ) : runningContainers.length === 0 ? (
            <div className="text-center py-8">
              <p className="text-muted-foreground mb-2">No running containers available.</p>
              <p className="text-sm text-muted-foreground">Deploy a stack first to access container terminals.</p>
            </div>
          ) : (
            <div className="grid gap-2">
              {runningContainers.map((container: Container) => (
                <button
                  key={container.id}
                  onClick={() => setSelectedContainer(container)}
                  className="flex items-center justify-between p-3 border rounded-md hover:bg-accent text-left transition-colors"
                >
                  <span className="font-medium">{container.name}</span>
                  <span className="text-sm text-success">running</span>
                </button>
              ))}
            </div>
          )}
        </div>
      ) : (
        <div className="h-full flex flex-col">
          <div className="flex-1 border rounded-lg overflow-hidden">
            <Terminal
              container={selectedContainer}
              onClose={() => setSelectedContainer(null)}
            />
          </div>
        </div>
      )}
    </div>
  );
}
