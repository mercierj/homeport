import { useQuery } from '@tanstack/react-query';
import { AWSOperationsEmptyState } from '@/components/aws-operations/AWSOperationsEmptyState';
import { AWSServiceGrid } from '@/components/aws-operations/AWSServiceGrid';
import { listAWSOperationServices, listAWSOperationsWorkspaces } from '@/lib/aws-operations-api';

export function AWSOperations() {
  const workspaces = useQuery({ queryKey: ['aws-operations', 'workspaces'], queryFn: listAWSOperationsWorkspaces });
  const workspace = workspaces.data?.workspaces.find((item) => Object.values(item.services).some((state) => state?.status === 'available'));
	const services = useQuery({ queryKey: ['aws-operations', workspace?.id, 'services'], queryFn: () => listAWSOperationServices(workspace!.id), enabled: Boolean(workspace) });
  if (workspaces.isLoading) return <p className="text-muted-foreground">Loading AWS operations…</p>;
  if (workspaces.isError) return <p className="text-destructive">Unable to load AWS operations.</p>;
  if (!workspace) return <AWSOperationsEmptyState />;
  return <div className="space-y-6"><div><h1 className="text-2xl font-bold">AWS operations</h1><p className="text-muted-foreground">Local post-cutover management for {workspace.name}. No AWS control-plane connection is used.</p></div><AWSServiceGrid workspace={workspace} services={services.data?.services} /></div>;
}
