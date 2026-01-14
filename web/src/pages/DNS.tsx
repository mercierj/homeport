import { DNSEditor } from '../components/DNSEditor';
import { toast } from 'sonner';

export function DNS() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">DNS</h1>
        <p className="text-muted-foreground">Manage DNS zones and records with PowerDNS</p>
      </div>
      <DNSEditor
        onError={(err) => toast.error(err.message)}
        onSuccess={(msg) => toast.success(msg)}
      />
    </div>
  );
}
