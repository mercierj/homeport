import { IdentityManager } from '../components/IdentityManager';
import { toast } from 'sonner';

export function Identity() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Identity</h1>
        <p className="text-muted-foreground">Manage users, roles, and authentication with Keycloak</p>
      </div>
      <IdentityManager onError={(err) => toast.error(err.message)} />
    </div>
  );
}
