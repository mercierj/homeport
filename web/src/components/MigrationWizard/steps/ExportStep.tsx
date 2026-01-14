import { useState } from 'react';
import {
  Package,
  Download,
  Settings,
  FileCode,
  FolderOpen,
  Lock,
  CheckCircle2,
  Loader2,
  ChevronDown,
  ChevronUp,
  Database,
  HardDrive,
  MessageSquare,
  Server,
  Settings2,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { buttonVariants } from '@/lib/button-variants';
import { useWizardStore } from '@/stores/wizard';
import { useMigrationConfigStore } from '@/stores/migration-config';
import {
  exportBundleWithProgress,
  downloadBundle,
  type ExportProgressEvent,
} from '@/lib/bundle-api';

export function ExportStep() {
  const {
    selectedResources,
    bundleId,
    bundleName,
    bundleManifest,
    domain,
    consolidate,
    isExporting,
    setBundleId,
    setBundleName,
    setBundleManifest,
    setSecretRefs,
    setDomain,
    setConsolidate,
    setIsExporting,
    setError,
    nextStep,
  } = useWizardStore();

  const [progress, setProgress] = useState<ExportProgressEvent | null>(null);
  const [detectSecrets, setDetectSecrets] = useState(true);
  const [includeMigration, setIncludeMigration] = useState(true);
  const [includeMonitoring, setIncludeMonitoring] = useState(false);
  const [showMigrationConfig, setShowMigrationConfig] = useState(false);

  // Migration config store
  const {
    options: migrationOptions,
    setOptions,
  } = useMigrationConfigStore();

  // Count resources by category for migration summary
  const resourceCounts = selectedResources.reduce((acc, r) => {
    const cat = r.category || 'other';
    acc[cat] = (acc[cat] || 0) + 1;
    return acc;
  }, {} as Record<string, number>);

  // Handle export
  const handleExport = async () => {
    setIsExporting(true);
    setError(null);

    try {
      const result = await exportBundleWithProgress(
        {
          resources: selectedResources,
          options: {
            domain,
            consolidate,
            detect_secrets: detectSecrets,
            include_migration: includeMigration,
            include_monitoring: includeMonitoring,
          },
        },
        setProgress
      );

      setBundleId(result.bundle_id);
      setBundleManifest(result.manifest);
      setBundleName(`migration-${new Date().toISOString().slice(0, 10)}.hprt`);

      // Set secret references from the API response
      if (result.secrets && result.secrets.length > 0) {
        setSecretRefs(result.secrets);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Export failed');
    } finally {
      setIsExporting(false);
      setProgress(null);
    }
  };

  // Handle download
  const handleDownload = async () => {
    if (!bundleId) return;

    try {
      const blob = await downloadBundle(bundleId);
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = bundleName;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Download failed');
    }
  };

  // Bundle preview content
  const bundlePreview = bundleManifest
    ? [
        { folder: 'compose/', files: ['docker-compose.yml', 'docker-compose.override.yml'] },
        { folder: 'configs/', files: ['nginx/', 'traefik/', 'postgres/'] },
        { folder: 'scripts/', files: ['pre-deploy.sh', 'post-deploy.sh', 'backup.sh', 'healthcheck.sh'] },
        { folder: 'migrations/', files: ['postgres/', 'redis/', 'rabbitmq/'] },
        { folder: 'data-sync/', files: ['sync-manifest.json', 'postgres-sync.sh', 's3-to-minio.sh'] },
        { folder: 'secrets/', files: ['.env.template', 'secrets-manifest.json', 'README.md'] },
        { folder: 'dns/', files: ['records.json', 'cutover.json'] },
        { folder: 'validation/', files: ['endpoints.json', 'expected-responses.json'] },
      ]
    : [];

  return (
    <div className="space-y-6">
      {/* Export options */}
      {!bundleManifest && (
        <>
          <div>
            <h3 className="text-lg font-semibold mb-4">Export Configuration</h3>
            <p className="text-muted-foreground mb-6">
              Configure your migration bundle. The bundle will contain all necessary
              files to deploy your infrastructure as Docker containers.
            </p>
          </div>

          {/* Domain setting */}
          <div>
            <label className="label">Domain Name (optional)</label>
            <input
              type="text"
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              className="input"
              placeholder="example.com"
            />
            <p className="text-sm text-muted-foreground mt-1">
              Used for Traefik routing and SSL certificates
            </p>
          </div>

          {/* Toggle options */}
          <div className="space-y-4">
            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={consolidate}
                onChange={(e) => setConsolidate(e.target.checked)}
                className="w-4 h-4 rounded border-border text-primary focus:ring-primary"
              />
              <div>
                <span className="font-medium">Consolidate Stacks</span>
                <p className="text-sm text-muted-foreground">
                  Merge similar resources into unified Docker Compose stacks
                </p>
              </div>
            </label>

            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={detectSecrets}
                onChange={(e) => setDetectSecrets(e.target.checked)}
                className="w-4 h-4 rounded border-border text-primary focus:ring-primary"
              />
              <div>
                <span className="font-medium">Detect Secret References</span>
                <p className="text-sm text-muted-foreground">
                  Scan for secrets and create references (values never stored in bundle)
                </p>
              </div>
            </label>

            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={includeMigration}
                onChange={(e) => setIncludeMigration(e.target.checked)}
                className="w-4 h-4 rounded border-border text-primary focus:ring-primary"
              />
              <div>
                <span className="font-medium">Include Migration Scripts</span>
                <p className="text-sm text-muted-foreground">
                  Generate data migration and sync scripts
                </p>
              </div>
            </label>

            {/* Expandable data migration config */}
            {includeMigration && (
              <div className="ml-7 border border-border rounded-lg overflow-hidden">
                <button
                  onClick={() => setShowMigrationConfig(!showMigrationConfig)}
                  className="w-full flex items-center justify-between px-4 py-3 bg-muted/50 hover:bg-muted transition-colors"
                >
                  <div className="flex items-center gap-2">
                    <Settings2 className="w-4 h-4 text-muted-foreground" />
                    <span className="text-sm font-medium">Configure Data Migration</span>
                  </div>
                  {showMigrationConfig ? (
                    <ChevronUp className="w-4 h-4 text-muted-foreground" />
                  ) : (
                    <ChevronDown className="w-4 h-4 text-muted-foreground" />
                  )}
                </button>

                {showMigrationConfig && (
                  <div className="p-4 space-y-4 border-t border-border bg-background">
                    {/* Resources to migrate summary */}
                    <div>
                      <p className="text-sm font-medium mb-2">Resources to Migrate</p>
                      <div className="flex flex-wrap gap-2">
                        {resourceCounts.database && resourceCounts.database > 0 && (
                          <span className="inline-flex items-center gap-1 px-2 py-1 bg-purple-500/10 text-purple-600 dark:text-purple-400 rounded text-xs">
                            <Database className="w-3 h-3" />
                            {resourceCounts.database} database{resourceCounts.database > 1 ? 's' : ''}
                          </span>
                        )}
                        {resourceCounts.storage && resourceCounts.storage > 0 && (
                          <span className="inline-flex items-center gap-1 px-2 py-1 bg-green-500/10 text-green-600 dark:text-green-400 rounded text-xs">
                            <HardDrive className="w-3 h-3" />
                            {resourceCounts.storage} storage
                          </span>
                        )}
                        {resourceCounts.messaging && resourceCounts.messaging > 0 && (
                          <span className="inline-flex items-center gap-1 px-2 py-1 bg-pink-500/10 text-pink-600 dark:text-pink-400 rounded text-xs">
                            <MessageSquare className="w-3 h-3" />
                            {resourceCounts.messaging} messaging
                          </span>
                        )}
                        {resourceCounts.compute && resourceCounts.compute > 0 && (
                          <span className="inline-flex items-center gap-1 px-2 py-1 bg-blue-500/10 text-blue-600 dark:text-blue-400 rounded text-xs">
                            <Server className="w-3 h-3" />
                            {resourceCounts.compute} compute
                          </span>
                        )}
                      </div>
                    </div>

                    {/* Global options */}
                    <div className="grid grid-cols-2 gap-3">
                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="checkbox"
                          checked={migrationOptions.dryRun}
                          onChange={(e) => setOptions({ dryRun: e.target.checked })}
                          className="w-4 h-4 rounded border-border text-primary"
                        />
                        <div>
                          <span className="text-sm">Dry Run</span>
                          <p className="text-xs text-muted-foreground">Preview only</p>
                        </div>
                      </label>

                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="checkbox"
                          checked={migrationOptions.continueOnError}
                          onChange={(e) => setOptions({ continueOnError: e.target.checked })}
                          className="w-4 h-4 rounded border-border text-primary"
                        />
                        <div>
                          <span className="text-sm">Continue on Error</span>
                          <p className="text-xs text-muted-foreground">Skip failed items</p>
                        </div>
                      </label>

                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="checkbox"
                          checked={migrationOptions.verifyAfterMigration}
                          onChange={(e) => setOptions({ verifyAfterMigration: e.target.checked })}
                          className="w-4 h-4 rounded border-border text-primary"
                        />
                        <div>
                          <span className="text-sm">Verify Data</span>
                          <p className="text-xs text-muted-foreground">Check integrity</p>
                        </div>
                      </label>

                      <label className="flex items-center gap-2 cursor-pointer">
                        <input
                          type="checkbox"
                          checked={migrationOptions.createBackup}
                          onChange={(e) => setOptions({ createBackup: e.target.checked })}
                          className="w-4 h-4 rounded border-border text-primary"
                        />
                        <div>
                          <span className="text-sm">Create Backup</span>
                          <p className="text-xs text-muted-foreground">Before migrate</p>
                        </div>
                      </label>
                    </div>

                    {/* Concurrency */}
                    <div>
                      <label className="text-sm font-medium">Max Concurrent Tasks</label>
                      <input
                        type="number"
                        min={1}
                        max={10}
                        value={migrationOptions.maxConcurrentTasks}
                        onChange={(e) => setOptions({ maxConcurrentTasks: parseInt(e.target.value) || 3 })}
                        className="input w-24 mt-1"
                      />
                    </div>
                  </div>
                )}
              </div>
            )}

            <label className="flex items-center gap-3 cursor-pointer">
              <input
                type="checkbox"
                checked={includeMonitoring}
                onChange={(e) => setIncludeMonitoring(e.target.checked)}
                className="w-4 h-4 rounded border-border text-primary focus:ring-primary"
              />
              <div>
                <span className="font-medium">Include Monitoring Stack</span>
                <p className="text-sm text-muted-foreground">
                  Add Prometheus, Grafana, and alerting configuration
                </p>
              </div>
            </label>
          </div>

          {/* Summary */}
          <div className="bg-muted/50 rounded-lg p-4">
            <h4 className="font-medium mb-3 flex items-center gap-2">
              <Settings className="w-4 h-4" />
              Bundle Summary
            </h4>
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
              <div>
                <p className="text-muted-foreground">Resources</p>
                <p className="font-medium">{selectedResources.length}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Consolidation</p>
                <p className="font-medium">{consolidate ? 'Enabled' : 'Disabled'}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Secret Detection</p>
                <p className="font-medium">{detectSecrets ? 'Enabled' : 'Disabled'}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Domain</p>
                <p className="font-medium">{domain || 'Not set'}</p>
              </div>
            </div>
          </div>

          {/* Export button */}
          <div className="flex justify-center pt-4">
            <button
              onClick={handleExport}
              disabled={isExporting}
              className={cn(
                buttonVariants({ variant: 'primary', size: 'lg' }),
                'gap-2',
                isExporting && 'opacity-50 cursor-not-allowed'
              )}
            >
              {isExporting ? (
                <>
                  <Loader2 className="w-5 h-5 animate-spin" />
                  Exporting...
                </>
              ) : (
                <>
                  <Package className="w-5 h-5" />
                  Create Bundle
                </>
              )}
            </button>
          </div>

          {/* Progress */}
          {isExporting && progress && (
            <div className="bg-muted/50 rounded-lg p-4">
              <div className="flex items-center gap-3 mb-2">
                <div className="w-2 h-2 bg-primary rounded-full animate-pulse" />
                <span className="text-sm font-medium">{progress.step}</span>
              </div>
              <p className="text-sm text-muted-foreground ml-5">{progress.message}</p>
              <div className="mt-2 ml-5">
                <div className="progress h-2">
                  <div
                    className="progress-indicator"
                    style={{ width: `${progress.progress}%` }}
                  />
                </div>
              </div>
            </div>
          )}
        </>
      )}

      {/* Bundle created - preview and download */}
      {bundleManifest && (
        <div className="space-y-6">
          <div className="flex items-center gap-3">
            <div className="w-12 h-12 rounded-lg bg-accent/10 flex items-center justify-center">
              <CheckCircle2 className="w-6 h-6 text-accent" />
            </div>
            <div>
              <h3 className="text-lg font-semibold">Bundle Created Successfully</h3>
              <p className="text-muted-foreground">
                Your migration bundle is ready for download
              </p>
            </div>
          </div>

          {/* Bundle info */}
          <div className="bg-card border border-border rounded-lg p-4">
            <div className="flex items-center justify-between mb-4">
              <div className="flex items-center gap-3">
                <Package className="w-5 h-5 text-primary" />
                <span className="font-medium">{bundleName}</span>
              </div>
              <button
                onClick={handleDownload}
                className={cn(buttonVariants({ variant: 'outline', size: 'sm' }), 'gap-2')}
              >
                <Download className="w-4 h-4" />
                Download
              </button>
            </div>

            <div className="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
              <div>
                <p className="text-muted-foreground">Version</p>
                <p className="font-medium">{bundleManifest.version}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Provider</p>
                <p className="font-medium capitalize">{bundleManifest.source.provider}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Resources</p>
                <p className="font-medium">{bundleManifest.source.resource_count}</p>
              </div>
              <div>
                <p className="text-muted-foreground">Stacks</p>
                <p className="font-medium">{bundleManifest.target.stack_count}</p>
              </div>
            </div>
          </div>

          {/* Bundle contents preview */}
          <div className="bg-muted/50 rounded-lg p-4">
            <h4 className="font-medium mb-3 flex items-center gap-2">
              <FolderOpen className="w-4 h-4" />
              Bundle Contents
            </h4>
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              {bundlePreview.map((item) => (
                <div key={item.folder} className="text-sm">
                  <p className="font-medium text-primary">{item.folder}</p>
                  <ul className="ml-4 text-muted-foreground">
                    {item.files.map((file) => (
                      <li key={file} className="flex items-center gap-1">
                        <FileCode className="w-3 h-3" />
                        {file}
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </div>
          </div>

          {/* Security notice */}
          <div className="bg-accent/5 border border-accent/20 rounded-lg p-4">
            <div className="flex items-start gap-3">
              <Lock className="w-5 h-5 text-accent flex-shrink-0" />
              <div>
                <p className="font-medium text-accent">No Secrets Included</p>
                <p className="text-sm text-muted-foreground mt-1">
                  This bundle contains only secret references, not actual values. You'll
                  provide secrets during the deployment step. The bundle is safe to store
                  in version control or share with your team.
                </p>
              </div>
            </div>
          </div>

          {/* Action buttons */}
          <div className="flex items-center justify-between pt-4 border-t border-border">
            <button
              onClick={() => {
                setBundleId(null);
                setBundleManifest(null);
              }}
              className={buttonVariants({ variant: 'outline' })}
            >
              Create New Bundle
            </button>
            <button
              onClick={nextStep}
              className={buttonVariants({ variant: 'primary' })}
            >
              Continue to Secrets
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
