import { useQuery } from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { listAWSOperationResources, listAWSOperationsWorkspaces } from '@/lib/aws-operations-api';

export function AWSService() {
  const { service = '' } = useParams();
  const workspaces = useQuery({ queryKey: ['aws-operations', 'workspaces'], queryFn: listAWSOperationsWorkspaces });
  const workspace = workspaces.data?.workspaces.find((item) => item.services[service]);
  const state = workspace?.services[service];
  const resources = useQuery({ queryKey: ['aws-operations', workspace?.id, service], queryFn: () => listAWSOperationResources<Record<string, unknown>>(workspace!.id, service), enabled: Boolean(workspace && service) });
  if (workspaces.isLoading) return <p className="text-muted-foreground">Loading AWS operations…</p>;
  if (!workspace || !state) return <div className="space-y-3"><p className="text-muted-foreground">This AWS service is not present in a migrated workspace.</p><Link className="text-primary underline" to="/aws">Back to AWS operations</Link></div>;
  return <div className="space-y-6"><div><Link className="text-sm text-primary underline" to="/aws">AWS operations</Link><h1 className="mt-2 text-2xl font-bold">{service} operations</h1><p className="text-muted-foreground">{state.reason || `Local target is ${state.status}. Capabilities: ${state.capabilities.join(', ') || 'none'}.`}</p></div><div className="table-wrapper"><table className="table"><thead className="table-header"><tr className="table-header-row"><th className="table-header-cell">Migrated resource</th><th className="table-header-cell">State</th><th className="table-header-cell">Local target</th></tr></thead><tbody className="table-body">{(resources.data?.resources ?? []).map((resource: Record<string, unknown>, index) => <tr key={String(resource.imported_resource_id ?? index)} className="table-row"><td className="table-cell">{String(resource.name ?? resource.imported_resource_id ?? 'Unknown')}</td><td className="table-cell">{String(resource.status ?? state.status)}</td><td className="table-cell">{String(resource.target ?? resource.local_stack_id ?? 'Local')}</td></tr>)}</tbody></table></div>{resources.isLoading && <p className="text-muted-foreground">Loading resources…</p>}</div>;
}
