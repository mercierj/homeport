import { SecretManager } from '../components/SecretManager';

export function Secrets() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Secrets</h1>
        <p className="text-muted-foreground">Manage secrets and environment variables</p>
      </div>
      <SecretManager stackId="default" />
    </div>
  );
}
