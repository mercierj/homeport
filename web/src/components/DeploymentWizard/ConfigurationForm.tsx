import { useState } from 'react';
import { useDeploymentStore } from '@/stores/deployment';
import { Loader2, Container, Box, Database, Bookmark } from 'lucide-react';
import type { ContainerRuntime } from '@/lib/deploy-api';

interface RDSDatabase {
  id: string;
  name: string;
  engine: string;
  endpoint?: string;
}

interface ConfigurationFormProps {
  onBack: () => void;
  onDeploy: () => void;
  onSaveForLater?: () => void;
  isDeploying: boolean;
  isSaving?: boolean;
  rdsDatabases?: RDSDatabase[];
}

export function ConfigurationForm({ onBack, onDeploy, onSaveForLater, isDeploying, isSaving = false, rdsDatabases = [] }: ConfigurationFormProps) {
  const { target, localConfig, sshConfig, updateLocalConfig, updateSSHConfig } = useDeploymentStore();
  const [showAdvanced, setShowAdvanced] = useState(false);
  const [rdsCredentials, setRdsCredentials] = useState<Record<string, { database: string; username: string; password: string }>>(
    () => Object.fromEntries(rdsDatabases.map(db => [db.id, { database: db.name, username: '', password: '' }]))
  );

  const updateRdsCredential = (id: string, field: 'database' | 'username' | 'password', value: string) => {
    setRdsCredentials(prev => ({
      ...prev,
      [id]: { ...prev[id], [field]: value }
    }));
  };

  const handleDeploy = () => {
    // Build RDS migration configs from credentials
    if (rdsDatabases.length > 0) {
      const rdsConfigs = rdsDatabases
        .filter(db => rdsCredentials[db.id]?.username && rdsCredentials[db.id]?.password)
        .map(db => ({
          identifier: db.name,
          engine: db.engine,
          endpoint: db.endpoint || '',
          database: rdsCredentials[db.id].database,
          username: rdsCredentials[db.id].username,
          password: rdsCredentials[db.id].password,
        }));

      if (target === 'local') {
        updateLocalConfig({ rdsDatabases: rdsConfigs.length > 0 ? rdsConfigs : undefined });
      }
    }
    onDeploy();
  };

  if (target === 'local') {
    return (
      <div className="space-y-6">
        <div className="text-center">
          <h2 className="text-2xl font-bold mb-2">Local Docker Configuration</h2>
          <p className="text-muted-foreground">Configure your local deployment options</p>
        </div>

        <div className="space-y-4 bg-muted rounded-lg p-6">
          <div>
            <label className="block text-sm font-medium mb-2">Container Runtime</label>
            <div className="grid grid-cols-3 gap-3">
              {(['auto', 'docker', 'podman'] as ContainerRuntime[]).map((rt) => (
                <button
                  key={rt}
                  type="button"
                  onClick={() => updateLocalConfig({ runtime: rt })}
                  className={`p-3 border rounded-lg flex flex-col items-center gap-2 transition-colors ${
                    localConfig.runtime === rt
                      ? 'border-accent bg-accent/10 text-accent'
                      : 'border-muted hover:border-muted-foreground/30'
                  }`}
                >
                  {rt === 'auto' ? (
                    <Box className="h-5 w-5" />
                  ) : (
                    <Container className="h-5 w-5" />
                  )}
                  <span className="text-sm font-medium capitalize">{rt}</span>
                  <span className="text-xs text-muted-foreground">
                    {rt === 'auto' && 'Auto-detect'}
                    {rt === 'docker' && 'Docker Engine'}
                    {rt === 'podman' && 'Podman (rootless)'}
                  </span>
                </button>
              ))}
            </div>
            <p className="text-xs text-muted-foreground mt-2">
              Both Docker and Podman produce OCI-compliant containers. Podman runs rootless by default.
            </p>
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Project Name</label>
            <input
              type="text"
              value={localConfig.projectName}
              onChange={(e) => updateLocalConfig({ projectName: e.target.value })}
              className="w-full px-3 py-2 border rounded-lg"
              placeholder="homeport-stack"
            />
            <p className="text-xs text-muted-foreground mt-1">
              Used for container project name and prefixes
            </p>
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Data Directory</label>
            <input
              type="text"
              value={localConfig.dataDirectory}
              onChange={(e) => updateLocalConfig({ dataDirectory: e.target.value })}
              className="w-full px-3 py-2 border rounded-lg"
              placeholder="~/.agnostech/data"
            />
            <p className="text-xs text-muted-foreground mt-1">
              Base directory for Docker volume mounts
            </p>
          </div>

          <div className="flex items-center gap-2">
            <input
              type="checkbox"
              id="autoStart"
              checked={localConfig.autoStart}
              onChange={(e) => updateLocalConfig({ autoStart: e.target.checked })}
              className="rounded"
            />
            <label htmlFor="autoStart" className="text-sm">
              Start containers automatically after creation
            </label>
          </div>

          <button
            type="button"
            onClick={() => setShowAdvanced(!showAdvanced)}
            className="text-sm text-accent hover:text-accent-hover"
          >
            {showAdvanced ? 'Hide' : 'Show'} advanced options
          </button>

          {showAdvanced && (
            <div className="space-y-4 pt-4 border-t">
              <div>
                <label className="block text-sm font-medium mb-1">Network Mode</label>
                <select
                  value={localConfig.networkMode}
                  onChange={(e) => updateLocalConfig({ networkMode: e.target.value as 'bridge' | 'host' })}
                  className="w-full px-3 py-2 border rounded-lg"
                >
                  <option value="bridge">Bridge (Recommended)</option>
                  <option value="host">Host</option>
                </select>
              </div>

              <div className="flex items-center gap-2">
                <input
                  type="checkbox"
                  id="enableMonitoring"
                  checked={localConfig.enableMonitoring}
                  onChange={(e) => updateLocalConfig({ enableMonitoring: e.target.checked })}
                  className="rounded"
                />
                <label htmlFor="enableMonitoring" className="text-sm">
                  Enable monitoring (Prometheus + Grafana)
                </label>
              </div>
            </div>
          )}
        </div>

        {/* RDS Database Credentials Section */}
        {rdsDatabases.length > 0 && (
          <div className="space-y-4 bg-muted rounded-lg p-6">
            <div className="flex items-center gap-2 mb-4">
              <Database className="h-5 w-5 text-purple-500" />
              <h3 className="font-medium">Database Credentials</h3>
              <span className="text-xs text-muted-foreground">({rdsDatabases.length} databases)</span>
            </div>
            <p className="text-sm text-muted-foreground mb-4">
              Provide credentials for each RDS database to migrate data. Credentials are only used for data export and are not stored.
            </p>

            {rdsDatabases.map((db) => (
              <div key={db.id} className="p-4 border rounded-lg space-y-3">
                <div className="flex items-center justify-between">
                  <span className="font-medium">{db.name}</span>
                  <span className="text-xs badge-outline">{db.engine}</span>
                </div>
                {db.endpoint && (
                  <p className="text-xs text-muted-foreground font-mono">{db.endpoint}</p>
                )}
                <div className="grid grid-cols-3 gap-3">
                  <div>
                    <label className="block text-xs font-medium mb-1">Database Name</label>
                    <input
                      type="text"
                      value={rdsCredentials[db.id]?.database || ''}
                      onChange={(e) => updateRdsCredential(db.id, 'database', e.target.value)}
                      className="w-full px-2 py-1.5 text-sm border rounded"
                      placeholder={db.name}
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium mb-1">Username</label>
                    <input
                      type="text"
                      value={rdsCredentials[db.id]?.username || ''}
                      onChange={(e) => updateRdsCredential(db.id, 'username', e.target.value)}
                      className="w-full px-2 py-1.5 text-sm border rounded"
                      placeholder="admin"
                    />
                  </div>
                  <div>
                    <label className="block text-xs font-medium mb-1">Password</label>
                    <input
                      type="password"
                      value={rdsCredentials[db.id]?.password || ''}
                      onChange={(e) => updateRdsCredential(db.id, 'password', e.target.value)}
                      className="w-full px-2 py-1.5 text-sm border rounded"
                    />
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}

        <div className="flex justify-between">
          <button onClick={onBack} className="px-4 py-2 text-muted-foreground hover:text-foreground">
            Back
          </button>
          <div className="flex gap-3">
            {onSaveForLater && (
              <button
                onClick={onSaveForLater}
                disabled={isSaving || isDeploying || !localConfig.projectName}
                className="px-4 py-2 border border-muted rounded-lg hover:bg-muted/50 disabled:opacity-50 flex items-center gap-2"
              >
                {isSaving ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin" />
                    Saving...
                  </>
                ) : (
                  <>
                    <Bookmark className="h-4 w-4" />
                    Save for Later
                  </>
                )}
              </button>
            )}
            <button
              onClick={handleDeploy}
              disabled={isDeploying || isSaving || !localConfig.projectName}
              className="px-6 py-2 bg-accent text-accent-foreground rounded-lg hover:bg-accent-hover disabled:opacity-50 flex items-center gap-2"
            >
              {isDeploying ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Starting...
                </>
              ) : (
                'Deploy Now'
              )}
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (target === 'ssh') {
    return (
      <div className="space-y-6">
        <div className="text-center">
          <h2 className="text-2xl font-bold mb-2">SSH Deployment Configuration</h2>
          <p className="text-muted-foreground">Configure your remote server connection</p>
        </div>

        <div className="space-y-4 bg-muted rounded-lg p-6">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label className="block text-sm font-medium mb-1">Host *</label>
              <input
                type="text"
                value={sshConfig.host}
                onChange={(e) => updateSSHConfig({ host: e.target.value })}
                className="w-full px-3 py-2 border rounded-lg"
                placeholder="server.example.com"
              />
            </div>
            <div>
              <label className="block text-sm font-medium mb-1">Port</label>
              <input
                type="number"
                value={sshConfig.port}
                onChange={(e) => updateSSHConfig({ port: parseInt(e.target.value) || 22 })}
                className="w-full px-3 py-2 border rounded-lg"
                placeholder="22"
              />
            </div>
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Username *</label>
            <input
              type="text"
              value={sshConfig.username}
              onChange={(e) => updateSSHConfig({ username: e.target.value })}
              className="w-full px-3 py-2 border rounded-lg"
              placeholder="deploy"
            />
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Authentication Method</label>
            <div className="flex gap-4">
              <label className="flex items-center gap-2">
                <input
                  type="radio"
                  name="authMethod"
                  checked={sshConfig.authMethod === 'key'}
                  onChange={() => updateSSHConfig({ authMethod: 'key' })}
                />
                SSH Key
              </label>
              <label className="flex items-center gap-2">
                <input
                  type="radio"
                  name="authMethod"
                  checked={sshConfig.authMethod === 'password'}
                  onChange={() => updateSSHConfig({ authMethod: 'password' })}
                />
                Password
              </label>
            </div>
          </div>

          {sshConfig.authMethod === 'key' && (
            <div>
              <label className="block text-sm font-medium mb-1">SSH Key Path</label>
              <input
                type="text"
                value={sshConfig.keyPath}
                onChange={(e) => updateSSHConfig({ keyPath: e.target.value })}
                className="w-full px-3 py-2 border rounded-lg"
                placeholder="~/.ssh/id_rsa"
              />
            </div>
          )}

          {sshConfig.authMethod === 'password' && (
            <div>
              <label className="block text-sm font-medium mb-1">Password</label>
              <input
                type="password"
                value={sshConfig.password}
                onChange={(e) => updateSSHConfig({ password: e.target.value })}
                className="w-full px-3 py-2 border rounded-lg"
              />
            </div>
          )}

          <div>
            <label className="block text-sm font-medium mb-1">Remote Directory</label>
            <input
              type="text"
              value={sshConfig.remoteDir}
              onChange={(e) => updateSSHConfig({ remoteDir: e.target.value })}
              className="w-full px-3 py-2 border rounded-lg"
              placeholder="/opt/agnostech"
            />
          </div>

          <div>
            <label className="block text-sm font-medium mb-1">Project Name</label>
            <input
              type="text"
              value={sshConfig.projectName}
              onChange={(e) => updateSSHConfig({ projectName: e.target.value })}
              className="w-full px-3 py-2 border rounded-lg"
              placeholder="homeport-stack"
            />
          </div>

          <div>
            <label className="block text-sm font-medium mb-2">Container Runtime</label>
            <div className="grid grid-cols-3 gap-3">
              {(['auto', 'docker', 'podman'] as ContainerRuntime[]).map((rt) => (
                <button
                  key={rt}
                  type="button"
                  onClick={() => updateSSHConfig({ runtime: rt })}
                  className={`p-3 border rounded-lg flex flex-col items-center gap-2 transition-colors ${
                    sshConfig.runtime === rt
                      ? 'border-accent bg-accent/10 text-accent'
                      : 'border-muted hover:border-muted-foreground/30'
                  }`}
                >
                  {rt === 'auto' ? (
                    <Box className="h-5 w-5" />
                  ) : (
                    <Container className="h-5 w-5" />
                  )}
                  <span className="text-sm font-medium capitalize">{rt}</span>
                  <span className="text-xs text-muted-foreground">
                    {rt === 'auto' && 'Auto-detect'}
                    {rt === 'docker' && 'Docker Engine'}
                    {rt === 'podman' && 'Podman (rootless)'}
                  </span>
                </button>
              ))}
            </div>
            <p className="text-xs text-muted-foreground mt-2">
              Runtime on the remote server. Auto will detect what's available.
            </p>
          </div>
        </div>

        <div className="alert-warning text-sm">
          <strong>Security note:</strong> Credentials are held in memory only and cleared after deployment.
        </div>

        <div className="flex justify-between">
          <button onClick={onBack} className="px-4 py-2 text-muted-foreground hover:text-foreground">
            Back
          </button>
          <div className="flex gap-3">
            {onSaveForLater && (
              <button
                onClick={onSaveForLater}
                disabled={isSaving || isDeploying || !sshConfig.host || !sshConfig.username}
                className="px-4 py-2 border border-muted rounded-lg hover:bg-muted/50 disabled:opacity-50 flex items-center gap-2"
              >
                {isSaving ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin" />
                    Saving...
                  </>
                ) : (
                  <>
                    <Bookmark className="h-4 w-4" />
                    Save for Later
                  </>
                )}
              </button>
            )}
            <button
              onClick={onDeploy}
              disabled={isDeploying || isSaving || !sshConfig.host || !sshConfig.username}
              className="px-6 py-2 bg-accent text-accent-foreground rounded-lg hover:bg-accent-hover disabled:opacity-50 flex items-center gap-2"
            >
              {isDeploying ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Connecting...
                </>
              ) : (
                'Deploy Now'
              )}
            </button>
          </div>
        </div>
      </div>
    );
  }

  return null;
}
