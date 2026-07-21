import type { AWSSQSResource } from '@/lib/aws-operations-types';
const count = (item: AWSSQSResource, key: 'pending' | 'active' | 'completed' | 'failed' | 'total'): number => {
  const value = item[`${key}_count` as keyof AWSSQSResource] ?? item[`${key[0].toUpperCase()}${key.slice(1)}Count` as keyof AWSSQSResource];
  return typeof value === 'number' ? value : 0;
};
export function SQSResourceList({ resources, onSelect }: { resources: AWSSQSResource[]; onSelect: (item: AWSSQSResource) => void }) { return <div className="space-y-2">{resources.map((queue) => <button key={queue.name} className="w-full rounded-lg border p-4 text-left hover:bg-muted/40" onClick={() => onSelect(queue)}><div className="flex justify-between"><span className="font-semibold">{queue.name}</span><span className="text-sm text-muted-foreground">{count(queue, 'total')} messages</span></div><p className="mt-2 text-sm text-muted-foreground">{count(queue, 'pending')} pending · {count(queue, 'failed')} failed · {queue.region || 'region unknown'}</p></button>)}</div>; }
