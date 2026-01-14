import { CertificateManager } from '../components/CertificateManager';
import { toast } from 'sonner';

export function Certificates() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Certificates</h1>
        <p className="text-muted-foreground">Manage SSL/TLS certificates</p>
      </div>
      <CertificateManager onError={(err) => toast.error(err.message)} />
    </div>
  );
}
