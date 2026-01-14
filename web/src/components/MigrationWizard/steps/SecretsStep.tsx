import { useState, useEffect, useRef } from 'react';
import {
  Key,
  Eye,
  EyeOff,
  CheckCircle2,
  AlertCircle,
  Cloud,
  FileText,
  Terminal,
  Loader2,
  ChevronUp,
  ChevronDown,
  Settings,
  Lock,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { buttonVariants } from '@/lib/button-variants';
import { useWizardStore } from '@/stores/wizard';
import { provideSecrets, pullSecrets, type PullSecretsRequest } from '@/lib/bundle-api';

// Secret source icons
const SOURCE_ICONS: Record<string, React.ElementType> = {
  manual: Key,
  env: Terminal,
  file: FileText,
  'aws-secrets-manager': Cloud,
  'gcp-secret-manager': Cloud,
  'azure-key-vault': Cloud,
  'hashicorp-vault': Key,
};

const SOURCE_LABELS: Record<string, string> = {
  manual: 'Manual Entry',
  env: 'Environment Variable',
  file: 'File',
  'aws-secrets-manager': 'AWS Secrets Manager',
  'gcp-secret-manager': 'GCP Secret Manager',
  'azure-key-vault': 'Azure Key Vault',
  'hashicorp-vault': 'HashiCorp Vault',
};

export function SecretsStep() {
  const {
    bundleId,
    secretRefs: rawSecretRefs,
    secretValues,
    setSecretValue,
    setSecretsResolved,
    setError,
    nextStep,
    // Cloud credentials
    awsCredentials,
    gcpCredentials,
    azureCredentials,
    setAwsCredentials,
    setGcpCredentials,
    setAzureCredentials,
  } = useWizardStore();

  // Ensure secretRefs is never null
  const secretRefs = rawSecretRefs ?? [];

  const [showSecrets, setShowSecrets] = useState<Record<string, boolean>>({});
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [isPulling, setIsPulling] = useState<string | null>(null);
  const [pullError, setPullError] = useState<string | null>(null);
  const [showCredentialsPanel, setShowCredentialsPanel] = useState(false);
  const [showResolvedSecrets, setShowResolvedSecrets] = useState(false);
  const autoPullRef = useRef(false);

  // Check if credentials are available for each provider
  const hasAwsCredentials = !!(awsCredentials.accessKeyId && awsCredentials.secretAccessKey);
  const hasGcpCredentials = !!(gcpCredentials.projectId);
  const hasAzureCredentials = !!(azureCredentials.subscriptionId && azureCredentials.clientId);

  // Handle pulling secrets from cloud provider
  const handlePullSecrets = async (provider: 'aws' | 'gcp' | 'azure') => {
    if (!bundleId) {
      setPullError('No bundle loaded');
      return;
    }

    setIsPulling(provider);
    setPullError(null);

    try {
      let request: PullSecretsRequest;

      switch (provider) {
        case 'aws':
          request = {
            provider: 'aws',
            access_key_id: awsCredentials.accessKeyId,
            secret_access_key: awsCredentials.secretAccessKey,
          };
          break;
        case 'gcp':
          request = {
            provider: 'gcp',
            project_id: gcpCredentials.projectId,
            service_account_json: gcpCredentials.serviceAccountJson,
          };
          break;
        case 'azure':
          request = {
            provider: 'azure',
            subscription_id: azureCredentials.subscriptionId,
            tenant_id: azureCredentials.tenantId,
            client_id: azureCredentials.clientId,
            client_secret: azureCredentials.clientSecret,
          };
          break;
      }

      const result = await pullSecrets(bundleId, request);

      // Populate resolved secrets into the form
      if (result.resolved && Object.keys(result.resolved).length > 0) {
        Object.entries(result.resolved).forEach(([name, value]) => {
          setSecretValue(name, value);
        });
      }

      // Show error if some failed
      if (result.failed && result.failed.length > 0) {
        const failedCount = result.failed.length;
        const resolvedCount = result.resolved ? Object.keys(result.resolved).length : 0;
        if (resolvedCount > 0) {
          setPullError(`Retrieved ${resolvedCount} secrets, ${failedCount} failed`);
        } else {
          setPullError(`Failed to retrieve ${failedCount} secrets. Check secret paths in your cloud provider.`);
        }
      }
    } catch (err) {
      setPullError(err instanceof Error ? err.message : 'Failed to pull secrets');
    } finally {
      setIsPulling(null);
    }
  };

  // Use actual secret refs from the bundle (no demo fallback)

  // Auto-pull secrets when entering the step if credentials are available from discovery
  useEffect(() => {
    if (autoPullRef.current || !bundleId || secretRefs.length === 0) {
      return;
    }

    const hasExistingValues = Object.keys(secretValues).some(
      (key) => secretValues[key] && secretValues[key].trim() !== ''
    );
    if (hasExistingValues) {
      return;
    }

    let providerToAutoPull: 'aws' | 'gcp' | 'azure' | null = null;
    if (hasAwsCredentials) {
      providerToAutoPull = 'aws';
    } else if (hasGcpCredentials) {
      providerToAutoPull = 'gcp';
    } else if (hasAzureCredentials) {
      providerToAutoPull = 'azure';
    }

    if (providerToAutoPull) {
      autoPullRef.current = true;
      handlePullSecrets(providerToAutoPull);
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bundleId, secretRefs.length, hasAwsCredentials, hasGcpCredentials, hasAzureCredentials]);

  const toggleShowSecret = (name: string) => {
    setShowSecrets((prev) => ({ ...prev, [name]: !prev[name] }));
  };

  // Check if all required secrets are provided
  const requiredSecrets = secretRefs.filter((s) => s.required);
  const providedRequiredSecrets = requiredSecrets.filter(
    (s) => secretValues[s.name] && secretValues[s.name].trim() !== ''
  );
  const allRequiredProvided = providedRequiredSecrets.length === requiredSecrets.length;

  // Debug: log what's happening
  console.log('[SecretsStep] Required:', requiredSecrets.length, 'Provided:', providedRequiredSecrets.length);
  console.log('[SecretsStep] secretValues keys:', Object.keys(secretValues));
  console.log('[SecretsStep] secretRefs names:', secretRefs.map(s => s.name));

  // Handle submit secrets
  const handleSubmit = async () => {
    if (!bundleId) {
      // Demo mode - just mark as resolved
      setSecretsResolved(true);
      nextStep();
      return;
    }

    setIsSubmitting(true);
    setError(null);

    try {
      const result = await provideSecrets(bundleId, {
        secrets: secretValues,
      });

      const missing = result.missing ?? [];
      if (missing.length > 0) {
        setError(`Missing required secrets: ${missing.join(', ')}`);
      } else {
        setSecretsResolved(true);
        nextStep();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to submit secrets');
    } finally {
      setIsSubmitting(false);
    }
  };

  // Handle skip (for optional secrets only)

  // Count secrets that can be pulled from cloud
  const cloudSecrets = secretRefs.filter(s =>
    s.source === 'aws-secrets-manager' ||
    s.source === 'gcp-secret-manager' ||
    s.source === 'azure-key-vault'
  );

  return (
    <div className="space-y-6">
      {/* Loading Modal */}
      {isPulling && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm">
          <div className="bg-card border border-border rounded-xl shadow-2xl p-8 max-w-md w-full mx-4 animate-in fade-in zoom-in-95">
            <div className="flex flex-col items-center text-center">
              <div className="relative mb-6">
                <div className="w-20 h-20 rounded-full bg-primary/10 flex items-center justify-center">
                  <Lock className="w-8 h-8 text-primary" />
                </div>
                <div className="absolute -bottom-1 -right-1 w-8 h-8 rounded-full bg-card border-2 border-border flex items-center justify-center">
                  <Loader2 className="w-4 h-4 text-accent animate-spin" />
                </div>
              </div>

              <h3 className="text-xl font-semibold mb-2">Fetching Secrets</h3>
              <p className="text-muted-foreground mb-6">
                Connecting to {isPulling === 'aws' ? 'AWS Secrets Manager' :
                  isPulling === 'gcp' ? 'GCP Secret Manager' : 'Azure Key Vault'}...
              </p>

              <div className="w-full space-y-3">
                <div className="flex items-center justify-between text-sm">
                  <span className="text-muted-foreground">Secrets to fetch:</span>
                  <span className="font-medium">{cloudSecrets.length}</span>
                </div>
                <div className="h-2 bg-muted rounded-full overflow-hidden relative">
                  <div
                    className="absolute inset-y-0 w-1/3 bg-accent rounded-full"
                    style={{
                      animation: 'indeterminate-progress 1.5s ease-in-out infinite',
                    }}
                  />
                  <style>{`
                    @keyframes indeterminate-progress {
                      0% { left: -33%; }
                      100% { left: 100%; }
                    }
                  `}</style>
                </div>
                <p className="text-xs text-muted-foreground">
                  This may take up to 30 seconds per secret...
                </p>
              </div>
            </div>
          </div>
        </div>
      )}

      <div>
        <h3 className="text-lg font-semibold mb-2">Provide Secret Values</h3>
        <p className="text-muted-foreground">
          The migration bundle contains references to secrets that need to be
          provided for deployment. Enter the values below - they will only be
          used during deployment and never stored in the bundle.
        </p>
      </div>

      {/* Security notice - only show when there are secrets */}
      {secretRefs.length > 0 && (
      <div className="bg-info/5 border border-info/20 rounded-lg p-4">
        <div className="flex items-start gap-3">
          <Key className="w-5 h-5 text-info flex-shrink-0" />
          <div>
            <p className="font-medium text-info">Secrets Handling</p>
            <p className="text-sm text-muted-foreground mt-1">
              Secrets are transmitted securely and stored only in memory during
              deployment. They are never written to disk or included in any logs.
            </p>
          </div>
        </div>
      </div>
      )}

      {/* Progress indicator - only show when there are secrets */}
      {secretRefs.length > 0 && !isPulling && (
      <div className="flex items-center gap-2 text-sm">
        <span className="font-medium">
          {providedRequiredSecrets.length} of {requiredSecrets.length} required
          secrets provided
        </span>
        {allRequiredProvided && (
          <CheckCircle2 className="w-4 h-4 text-accent" />
        )}
        {cloudSecrets.length > 0 && (
          <span className="text-muted-foreground ml-2">
            ({cloudSecrets.length} can be auto-fetched)
          </span>
        )}
      </div>
      )}

      {/* No secrets required message */}
      {secretRefs.length === 0 && (
        <div className="bg-accent/10 border border-accent/20 rounded-lg p-6 text-center">
          <CheckCircle2 className="w-12 h-12 text-accent mx-auto mb-3" />
          <h4 className="font-semibold text-lg mb-2">No Secrets Required</h4>
          <p className="text-muted-foreground">
            This migration bundle doesn't require any secret values.
            You can proceed directly to deployment.
          </p>
        </div>
      )}

      {/* Secret inputs - separated into missing and resolved */}
      {secretRefs.length > 0 && (() => {
        // Separate secrets into missing and resolved
        const missingSecrets = secretRefs.filter(
          (s) => !secretValues[s.name] || secretValues[s.name].trim() === ''
        );
        const resolvedSecrets = secretRefs.filter(
          (s) => secretValues[s.name] && secretValues[s.name].trim() !== ''
        );

        const renderSecretCard = (secret: typeof secretRefs[0]) => {
          const Icon = SOURCE_ICONS[secret.source] || Key;
          const isProvided =
            secretValues[secret.name] && secretValues[secret.name].trim() !== '';
          const showValue = showSecrets[secret.name];

          return (
            <div
              key={secret.name}
              className={cn(
                'bg-card border rounded-lg p-4',
                isProvided ? 'border-accent/50' : 'border-border'
              )}
            >
              <div className="flex items-start justify-between mb-3">
                <div className="flex items-start gap-3">
                  <div className="p-2 rounded-lg bg-muted">
                    <Icon className="w-4 h-4" />
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <span className="font-mono font-medium">{secret.name}</span>
                      {secret.required ? (
                        <span className="badge-error text-xs">Required</span>
                      ) : (
                        <span className="badge-outline text-xs">Optional</span>
                      )}
                      {isProvided && (
                        <CheckCircle2 className="w-4 h-4 text-accent" />
                      )}
                    </div>
                    <p className="text-sm text-muted-foreground mt-1">
                      {secret.description}
                    </p>
                    {secret.key && (
                      <p className="text-xs text-muted-foreground mt-1">
                        Source: {SOURCE_LABELS[secret.source]} ({secret.key})
                      </p>
                    )}
                  </div>
                </div>
              </div>

              <div className="relative">
                <input
                  type={showValue ? 'text' : 'password'}
                  value={secretValues[secret.name] || ''}
                  onChange={(e) => setSecretValue(secret.name, e.target.value)}
                  className="input pr-10 font-mono"
                  placeholder={`Enter ${secret.name}`}
                />
                <button
                  type="button"
                  onClick={() => toggleShowSecret(secret.name)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                >
                  {showValue ? (
                    <EyeOff className="w-4 h-4" />
                  ) : (
                    <Eye className="w-4 h-4" />
                  )}
                </button>
              </div>
            </div>
          );
        };

        return (
          <div className="space-y-6">
            {/* Missing secrets section */}
            {missingSecrets.length > 0 && (
              <div className="space-y-3">
                <div className="flex items-center gap-2 px-1">
                  <AlertCircle className="w-4 h-4 text-warning" />
                  <h4 className="font-medium text-warning">
                    Missing Secrets ({missingSecrets.length})
                  </h4>
                </div>
                <div className="space-y-3">
                  {missingSecrets.map(renderSecretCard)}
                </div>
              </div>
            )}

            {/* Resolved secrets section - collapsible */}
            {resolvedSecrets.length > 0 && (
              <div className="space-y-3">
                <button
                  onClick={() => setShowResolvedSecrets(!showResolvedSecrets)}
                  className="flex items-center gap-2 px-1 w-full text-left hover:opacity-80 transition-opacity"
                >
                  <CheckCircle2 className="w-4 h-4 text-accent" />
                  <h4 className="font-medium text-accent">
                    Resolved Secrets ({resolvedSecrets.length})
                  </h4>
                  {showResolvedSecrets ? (
                    <ChevronUp className="w-4 h-4 text-muted-foreground ml-auto" />
                  ) : (
                    <ChevronDown className="w-4 h-4 text-muted-foreground ml-auto" />
                  )}
                </button>
                {showResolvedSecrets && (
                  <div className="space-y-3">
                    {resolvedSecrets.map(renderSecretCard)}
                  </div>
                )}
              </div>
            )}
          </div>
        );
      })()}

      {/* Pull from cloud option - only show when there are secrets */}
      {secretRefs.length > 0 && (
      <div className="bg-muted/50 rounded-lg p-4">
        <h4 className="font-medium mb-3">Pull from Cloud Provider</h4>
        <p className="text-sm text-muted-foreground mb-4">
          Automatically retrieve secrets from your cloud provider's secret manager.
        </p>

        {/* Credentials configuration panel */}
        {!showCredentialsPanel && (
          <button
            onClick={() => setShowCredentialsPanel(true)}
            className={cn(buttonVariants({ variant: 'outline', size: 'sm' }), 'gap-2 mb-4')}
          >
            <Settings className="w-4 h-4" />
            {hasAwsCredentials || hasGcpCredentials || hasAzureCredentials
              ? 'Edit Cloud Credentials'
              : 'Configure Cloud Credentials'}
          </button>
        )}

        {showCredentialsPanel && (
          <div className="mb-4 space-y-4 border border-border rounded-lg p-4 bg-card">
            <div className="flex items-center justify-between">
              <h5 className="font-medium text-sm">Cloud Credentials</h5>
              <button
                onClick={() => setShowCredentialsPanel(false)}
                className="text-muted-foreground hover:text-foreground"
              >
                <ChevronUp className="w-4 h-4" />
              </button>
            </div>

            {/* AWS Credentials */}
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">AWS</span>
                {hasAwsCredentials && <CheckCircle2 className="w-3 h-3 text-accent" />}
              </div>
              <div className="grid grid-cols-2 gap-2">
                <input
                  type="text"
                  value={awsCredentials.accessKeyId}
                  onChange={(e) => setAwsCredentials({ accessKeyId: e.target.value })}
                  className="input text-sm"
                  placeholder="Access Key ID"
                />
                <input
                  type="password"
                  value={awsCredentials.secretAccessKey}
                  onChange={(e) => setAwsCredentials({ secretAccessKey: e.target.value })}
                  className="input text-sm"
                  placeholder="Secret Access Key"
                />
              </div>
            </div>

            {/* GCP Credentials */}
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">GCP</span>
                {hasGcpCredentials && <CheckCircle2 className="w-3 h-3 text-accent" />}
              </div>
              <input
                type="text"
                value={gcpCredentials.projectId}
                onChange={(e) => setGcpCredentials({ projectId: e.target.value })}
                className="input text-sm"
                placeholder="Project ID"
              />
              <textarea
                value={gcpCredentials.serviceAccountJson}
                onChange={(e) => setGcpCredentials({ serviceAccountJson: e.target.value })}
                className="input text-sm min-h-[60px] font-mono"
                placeholder="Service Account JSON (paste content)"
              />
            </div>

            {/* Azure Credentials */}
            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">Azure</span>
                {hasAzureCredentials && <CheckCircle2 className="w-3 h-3 text-accent" />}
              </div>
              <div className="grid grid-cols-2 gap-2">
                <input
                  type="text"
                  value={azureCredentials.subscriptionId}
                  onChange={(e) => setAzureCredentials({ subscriptionId: e.target.value })}
                  className="input text-sm"
                  placeholder="Subscription ID"
                />
                <input
                  type="text"
                  value={azureCredentials.tenantId}
                  onChange={(e) => setAzureCredentials({ tenantId: e.target.value })}
                  className="input text-sm"
                  placeholder="Tenant ID"
                />
                <input
                  type="text"
                  value={azureCredentials.clientId}
                  onChange={(e) => setAzureCredentials({ clientId: e.target.value })}
                  className="input text-sm"
                  placeholder="Client ID"
                />
                <input
                  type="password"
                  value={azureCredentials.clientSecret}
                  onChange={(e) => setAzureCredentials({ clientSecret: e.target.value })}
                  className="input text-sm"
                  placeholder="Client Secret"
                />
              </div>
            </div>
          </div>
        )}

        {!bundleId && (
          <p className="text-sm text-warning mb-3 flex items-center gap-2">
            <AlertCircle className="w-4 h-4" />
            No bundle loaded - upload or export a bundle first to pull secrets from cloud providers.
          </p>
        )}

        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            onClick={() => handlePullSecrets('aws')}
            disabled={!hasAwsCredentials || isPulling !== null}
            className={cn(
              buttonVariants({ variant: hasAwsCredentials ? 'aws' : 'outline', size: 'sm' }),
              'gap-2',
              !hasAwsCredentials && 'opacity-50'
            )}
          >
            {isPulling === 'aws' ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <Cloud className="w-4 h-4" />
            )}
            AWS Secrets Manager
            {hasAwsCredentials && <CheckCircle2 className="w-3 h-3" />}
          </button>
          <button
            type="button"
            onClick={() => handlePullSecrets('gcp')}
            disabled={!hasGcpCredentials || isPulling !== null}
            className={cn(
              buttonVariants({ variant: hasGcpCredentials ? 'gcp' : 'outline', size: 'sm' }),
              'gap-2',
              !hasGcpCredentials && 'opacity-50'
            )}
          >
            {isPulling === 'gcp' ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <Cloud className="w-4 h-4" />
            )}
            GCP Secret Manager
            {hasGcpCredentials && <CheckCircle2 className="w-3 h-3" />}
          </button>
          <button
            type="button"
            onClick={() => handlePullSecrets('azure')}
            disabled={!hasAzureCredentials || isPulling !== null}
            className={cn(
              buttonVariants({ variant: hasAzureCredentials ? 'azure' : 'outline', size: 'sm' }),
              'gap-2',
              !hasAzureCredentials && 'opacity-50'
            )}
          >
            {isPulling === 'azure' ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <Cloud className="w-4 h-4" />
            )}
            Azure Key Vault
            {hasAzureCredentials && <CheckCircle2 className="w-3 h-3" />}
          </button>
        </div>
        {pullError && (
          <p className="text-xs text-error mt-2">{pullError}</p>
        )}
        {(hasAwsCredentials || hasGcpCredentials || hasAzureCredentials) && (
          <p className="text-xs text-muted-foreground mt-2">
            Click a provider button to pull secrets using the credentials from your discovery scan.
          </p>
        )}
      </div>
      )}

      {/* Action buttons */}
      <div className="flex items-center justify-between pt-4 border-t border-border">
        {secretRefs.length > 0 && !allRequiredProvided && (
          <div className="flex items-center gap-2 text-sm text-warning">
            <AlertCircle className="w-4 h-4" />
            <span>Please provide all required secrets</span>
          </div>
        )}
        <div className="flex-1" />
        <button
          onClick={handleSubmit}
          disabled={!allRequiredProvided || isSubmitting}
          className={cn(
            buttonVariants({ variant: 'primary' }),
            (!allRequiredProvided || isSubmitting) && 'opacity-50 cursor-not-allowed'
          )}
        >
          {isSubmitting ? 'Validating...' : 'Continue to Deploy'}
        </button>
      </div>
    </div>
  );
}
