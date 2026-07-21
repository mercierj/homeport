import type { AWSOperationServiceState } from '@/lib/aws-operations-types';
export function AWSServiceUnavailable({ name, state }: { name: string; state?: AWSOperationServiceState }) {
  return <div className="rounded-xl border bg-muted/30 p-5"><div className="flex items-center justify-between"><h2 className="font-semibold">{name}</h2><span className="rounded-full bg-muted px-2 py-1 text-xs">Unavailable</span></div><p className="mt-3 text-sm text-muted-foreground">{state?.reason || 'This service has not been activated after cutover.'}</p></div>;
}
