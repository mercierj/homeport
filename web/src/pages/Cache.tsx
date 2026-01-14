import { CacheBrowser } from '../components/CacheBrowser';

export function Cache() {
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Cache</h1>
        <p className="text-muted-foreground">Browse and manage Redis cache</p>
      </div>
      <CacheBrowser stackId="default" />
    </div>
  );
}
