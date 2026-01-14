import { Monitor, Server, Download, Cloud } from 'lucide-react';
import type { DeployTarget } from '@/lib/deploy-api';

interface TargetSelectorProps {
  onSelect: (target: DeployTarget | 'export') => void;
}

export function TargetSelector({ onSelect }: TargetSelectorProps) {
  return (
    <div className="space-y-6">
      <div className="text-center">
        <h2 className="text-2xl font-bold mb-2">How would you like to deploy?</h2>
        <p className="text-muted-foreground">
          Choose a deployment target for your self-hosted stack
        </p>
      </div>

      <div className="grid grid-cols-3 gap-4">
        <button
          onClick={() => onSelect('local')}
          className="card-action p-6 rounded-xl border-2 text-left group"
        >
          <Monitor className="h-10 w-10 mb-4 text-emerald-600" />
          <h3 className="text-lg font-semibold mb-1">Local Docker</h3>
          <p className="text-sm text-muted-foreground">
            Deploy to Docker on this machine. Requires Docker Desktop or Docker Engine.
          </p>
        </button>

        <button
          onClick={() => onSelect('ssh')}
          className="card-action p-6 rounded-xl border-2 text-left group"
        >
          <Server className="h-10 w-10 mb-4 text-primary" />
          <h3 className="text-lg font-semibold mb-1">Remote SSH</h3>
          <p className="text-sm text-muted-foreground">
            Deploy to a remote server via SSH. Requires SSH access and Docker on the server.
          </p>
        </button>

        <button
          onClick={() => onSelect('cloud')}
          className="card-action p-6 rounded-xl border-2 text-left group relative"
        >
          <div className="absolute top-3 right-3">
            <span className="badge-freedom text-xs">EU GDPR</span>
          </div>
          <Cloud className="h-10 w-10 mb-4 text-accent" />
          <h3 className="text-lg font-semibold mb-1">Cloud Provider</h3>
          <p className="text-sm text-muted-foreground">
            Deploy to Hetzner, Scaleway, or OVH. Compare prices and save up to 70%.
          </p>
        </button>
      </div>

      <div className="pt-4 border-t">
        <button
          onClick={() => onSelect('export')}
          className="card-action w-full p-4 rounded-lg flex items-center gap-3"
        >
          <Download className="h-5 w-5 text-gray-500" />
          <div className="text-left">
            <div className="font-medium">Export ZIP</div>
            <div className="text-sm text-muted-foreground">
              Download configuration files for manual deployment
            </div>
          </div>
        </button>
      </div>
    </div>
  );
}
