import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { api } from '@/lib/api';
import { listDiscoveries } from '@/lib/migrate-api';
import { listStacks, getPendingDeployments, formatCost, providerDisplayNames, type Stack } from '@/lib/stacks-api';
import { ContainerList } from '@/components/ContainerList';
import { LogViewer } from '@/components/LogViewer';
import { Button } from '@/components/ui/button';
import { Layers, Rocket, Settings2 } from 'lucide-react';
import type { Container } from '@/lib/docker-api';

export function Dashboard() {
  const navigate = useNavigate();
  const [selectedContainer, setSelectedContainer] = useState<Container | null>(null);

  const { data: health, isLoading: healthLoading } = useQuery({
    queryKey: ['health', 'detailed'],
    queryFn: api.healthDetailed,
    refetchInterval: 30000,
  });

  const { data: discoveries } = useQuery({
    queryKey: ['discoveries'],
    queryFn: listDiscoveries,
  });

  const { data: stacksData } = useQuery({
    queryKey: ['stacks'],
    queryFn: listStacks,
    refetchInterval: 30000,
  });

  const pendingDeployments = stacksData?.stacks ? getPendingDeployments(stacksData.stacks) : [];

  return (
    <div className="space-y-6">
      <h1 className="text-3xl font-bold">Dashboard</h1>

      <div className="grid gap-4 md:grid-cols-3">
        <div className="card-stat">
          <p className="card-stat-label">API Status</p>
          <p className={`card-stat-value ${
            health?.status === 'healthy' ? 'text-success' :
            health?.status === 'degraded' ? 'text-warning' : ''
          }`}>
            {healthLoading ? 'Loading...' : health?.status || 'Unknown'}
          </p>
        </div>

        <div className="card-stat">
          <p className="card-stat-label">Discoveries</p>
          <p className="card-stat-value">{discoveries?.length ?? 0}</p>
        </div>

        <div className="card-stat">
          <p className="card-stat-label">Docker</p>
          {healthLoading ? (
            <p className="card-stat-value text-muted-foreground">Loading...</p>
          ) : (
            <p className={`card-stat-value ${
              health?.dependencies?.docker?.status === 'healthy' ? 'text-success' :
              health?.dependencies?.docker?.status === 'degraded' ? 'text-warning' :
              'text-error'
            }`}>
              {health?.dependencies?.docker?.status === 'healthy' ? 'Connected' :
               health?.dependencies?.docker?.error || 'Disconnected'}
            </p>
          )}
        </div>
      </div>

      {/* Pending Deployments Widget */}
      {pendingDeployments.length > 0 && (
        <div className="rounded-lg border p-4">
          <div className="flex items-center gap-2 mb-4">
            <Layers className="h-5 w-5 text-primary" />
            <h2 className="text-lg font-semibold">Pending Deployments</h2>
            <span className="badge-info ml-2">{pendingDeployments.length}</span>
          </div>
          <div className="space-y-3">
            {pendingDeployments.map((stack: Stack) => (
              <div
                key={stack.id}
                className="flex items-center justify-between p-3 rounded-lg border bg-muted/30 hover:bg-muted/50 transition-colors"
              >
                <div className="flex items-center gap-3">
                  <div className="resource-icon-compute">
                    <Layers className="h-4 w-4" />
                  </div>
                  <div>
                    <p className="font-medium">{stack.name}</p>
                    <p className="text-sm text-muted-foreground">
                      {stack.deployment_config && providerDisplayNames[stack.deployment_config.provider] || stack.deployment_config?.provider}
                      {stack.deployment_config?.region && ` • ${stack.deployment_config.region}`}
                    </p>
                  </div>
                </div>
                <div className="flex items-center gap-3">
                  {stack.deployment_config?.estimated_cost && (
                    <span className="text-sm font-medium text-success">
                      {formatCost(stack.deployment_config.estimated_cost)}
                    </span>
                  )}
                  <div className="flex gap-1">
                    <Button
                      size="sm"
                      onClick={() => navigate(`/deploy?stack=${stack.id}`)}
                    >
                      <Rocket className="h-4 w-4 mr-1" />
                      Deploy
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => navigate(`/stacks?edit=${stack.id}`)}
                    >
                      <Settings2 className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

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
