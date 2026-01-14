import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import type { Container } from '@/lib/docker-api';
import { listContainers, restartContainer, stopContainer, startContainer } from '@/lib/docker-api';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { RefreshCw, Square, Play, RotateCcw, Terminal } from 'lucide-react';
import { statusBadgeClasses } from '@/lib/diagram-types';

interface ContainerListProps {
  stackId?: string;
  onViewLogs?: (container: Container) => void;
}

export function ContainerList({ stackId = 'default', onViewLogs }: ContainerListProps) {
  const queryClient = useQueryClient();

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['containers', stackId],
    queryFn: () => listContainers(stackId),
    refetchInterval: 5000,
  });

  const restartMutation = useMutation({
    mutationFn: (name: string) => restartContainer(stackId, name),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['containers'] }),
  });

  const stopMutation = useMutation({
    mutationFn: (name: string) => stopContainer(stackId, name),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['containers'] }),
  });

  const startMutation = useMutation({
    mutationFn: (name: string) => startContainer(stackId, name),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['containers'] }),
  });

  if (isLoading) {
    return <div className="text-muted-foreground">Loading containers...</div>;
  }

  if (error) {
    return (
      <div className="text-error">
        Error loading containers. Is Docker running?
      </div>
    );
  }

  const containers = data?.containers || [];

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Containers ({containers.length})</h2>
        <Button variant="outline" size="sm" onClick={() => refetch()}>
          <RefreshCw className="h-4 w-4 mr-2" />
          Refresh
        </Button>
      </div>

      {containers.length === 0 ? (
        <div className="empty-state">
          <p className="empty-state-description">No containers found</p>
        </div>
      ) : (
        <div className="space-y-2">
          {containers.map((container) => (
            <div
              key={container.id}
              className="card-resource"
            >
              <div className="flex items-center gap-4">
                <span className={cn(
                  statusBadgeClasses[container.state] || 'badge-secondary'
                )}>
                  {container.state}
                </span>
                <div>
                  <p className="font-medium">{container.name}</p>
                  <p className="text-sm text-muted-foreground">{container.image}</p>
                </div>
              </div>

              <div className="flex items-center gap-2">
                {container.state === 'running' ? (
                  <>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => onViewLogs?.(container)}
                    >
                      <Terminal className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => restartMutation.mutate(container.name)}
                      disabled={restartMutation.isPending}
                    >
                      <RotateCcw className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => stopMutation.mutate(container.name)}
                      disabled={stopMutation.isPending}
                    >
                      <Square className="h-4 w-4" />
                    </Button>
                  </>
                ) : (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => startMutation.mutate(container.name)}
                    disabled={startMutation.isPending}
                  >
                    <Play className="h-4 w-4" />
                  </Button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
