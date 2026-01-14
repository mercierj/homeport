import { ArrowLeft, Globe, Shield, Activity, Archive } from 'lucide-react';
import { RegionSelector } from './RegionSelector';
import { HALevelSelector } from './HALevelSelector';
import { useDeploymentStore, type HALevel } from '@/stores/deployment';
import { buttonVariants } from '@/lib/button-variants';
import type { Provider, Region } from '@/lib/providers-api';

interface ProviderConfigFormProps {
  provider: Provider;
  baseCost: number;
  onBack: () => void;
  onDeploy: () => void;
}

export function ProviderConfigForm({
  provider,
  baseCost,
  onBack,
  onDeploy,
}: ProviderConfigFormProps) {
  const { cloudConfig, updateCloudConfig } = useDeploymentStore();

  const handleRegionChange = (region: Region) => {
    updateCloudConfig({ region });
  };

  const handleHALevelChange = (level: HALevel) => {
    updateCloudConfig({ haLevel: level });
  };

  return (
    <div className="space-y-6">
      {/* Region Selection */}
      <RegionSelector
        provider={provider}
        selectedRegion={cloudConfig.region}
        onSelect={handleRegionChange}
      />

      {/* HA Level Selection */}
      <HALevelSelector
        selectedLevel={cloudConfig.haLevel}
        baseCost={baseCost}
        onSelect={handleHALevelChange}
      />

      {/* Domain & SSL Section */}
      <div className="p-4 border rounded-lg space-y-4">
        <div className="flex items-center gap-2 mb-2">
          <Globe className="h-5 w-5 text-muted-foreground" />
          <h3 className="font-medium">Domain & SSL</h3>
        </div>

        <div>
          <label className="label">Domain Name</label>
          <input
            type="text"
            value={cloudConfig.domain}
            onChange={(e) => updateCloudConfig({ domain: e.target.value })}
            className="input w-full"
            placeholder="example.com"
          />
          <p className="text-xs text-muted-foreground mt-1">
            Your custom domain for accessing the deployment
          </p>
        </div>

        <label className="flex items-center justify-between cursor-pointer">
          <div className="flex items-center gap-3">
            <Shield className="h-5 w-5 text-muted-foreground" />
            <div>
              <p className="font-medium">Enable SSL</p>
              <p className="text-sm text-muted-foreground">Secure with Let's Encrypt</p>
            </div>
          </div>
          <div
            className={`relative w-11 h-6 rounded-full transition-colors ${
              cloudConfig.enableSSL ? 'bg-accent' : 'bg-gray-300'
            }`}
          >
            <div
              className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${
                cloudConfig.enableSSL ? 'translate-x-5' : ''
              }`}
            />
            <input
              type="checkbox"
              className="sr-only"
              checked={cloudConfig.enableSSL}
              onChange={(e) => updateCloudConfig({ enableSSL: e.target.checked })}
            />
          </div>
        </label>
      </div>

      {/* Additional Options Section */}
      <div className="p-4 border rounded-lg space-y-4">
        <h3 className="font-medium mb-2">Additional Options</h3>

        <label className="flex items-center justify-between cursor-pointer">
          <div className="flex items-center gap-3">
            <Activity className="h-5 w-5 text-muted-foreground" />
            <div>
              <p className="font-medium">Include Monitoring</p>
              <p className="text-sm text-muted-foreground">Prometheus + Grafana stack</p>
            </div>
          </div>
          <div
            className={`relative w-11 h-6 rounded-full transition-colors ${
              cloudConfig.enableMonitoring ? 'bg-accent' : 'bg-gray-300'
            }`}
          >
            <div
              className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${
                cloudConfig.enableMonitoring ? 'translate-x-5' : ''
              }`}
            />
            <input
              type="checkbox"
              className="sr-only"
              checked={cloudConfig.enableMonitoring}
              onChange={(e) => updateCloudConfig({ enableMonitoring: e.target.checked })}
            />
          </div>
        </label>

        <label className="flex items-center justify-between cursor-pointer">
          <div className="flex items-center gap-3">
            <Archive className="h-5 w-5 text-muted-foreground" />
            <div>
              <p className="font-medium">Include Backups</p>
              <p className="text-sm text-muted-foreground">Automated daily backups</p>
            </div>
          </div>
          <div
            className={`relative w-11 h-6 rounded-full transition-colors ${
              cloudConfig.enableBackups ? 'bg-accent' : 'bg-gray-300'
            }`}
          >
            <div
              className={`absolute top-0.5 left-0.5 w-5 h-5 rounded-full bg-white transition-transform ${
                cloudConfig.enableBackups ? 'translate-x-5' : ''
              }`}
            />
            <input
              type="checkbox"
              className="sr-only"
              checked={cloudConfig.enableBackups}
              onChange={(e) => updateCloudConfig({ enableBackups: e.target.checked })}
            />
          </div>
        </label>
      </div>

      {/* Action Buttons */}
      <div className="flex justify-between pt-4 border-t">
        <button
          onClick={onBack}
          className={buttonVariants({ variant: 'ghost', size: 'default' })}
        >
          <ArrowLeft className="h-4 w-4 mr-2" />
          Back
        </button>
        <button
          onClick={onDeploy}
          disabled={!cloudConfig.region}
          className={buttonVariants({ variant: 'freedom', size: 'default' })}
        >
          Deploy to {provider.charAt(0).toUpperCase() + provider.slice(1)}
        </button>
      </div>
    </div>
  );
}
