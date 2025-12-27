import { useState, useEffect } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { FileDropzone } from '@/components/FileDropzone';
import { ArchitectureDiagram } from '../components/ArchitectureDiagram';
import { ResourceDetailPanel } from '../components/ResourceDetailPanel';
import { SelectionSummary } from '../components/SelectionSummary';
import { Button } from '@/components/ui/button';
import {
  TargetSelector,
  ConfigurationForm,
  DeploymentExecution,
} from '@/components/DeploymentWizard';
import { MigrationConfigStep } from '@/components/MigrationWizard';
import { useDeploymentStore } from '@/stores/deployment';
import {
  analyzeFiles,
  generateStack,
  downloadStack,
  discoverInfrastructure,
  listDiscoveries,
  getDiscovery,
  saveDiscovery,
  deleteDiscovery,
  type Resource,
  type DiscoverRequest,
  type AnalyzeResponse,
} from '@/lib/migrate-api';
import { startDeployment, type DeployTarget } from '@/lib/deploy-api';
import { Loader2, ArrowLeft, Upload, Cloud, Save, History, Trash2, FolderOpen } from 'lucide-react';

type Step = 'upload' | 'review' | 'migration-config' | 'target' | 'configure' | 'deploy';
type SourceType = 'file' | 'api';
type Provider = 'aws' | 'gcp' | 'azure';

export function Migrate() {
  const queryClient = useQueryClient();
  const [step, setStep] = useState<Step>('upload');
  const [resources, setResources] = useState<Resource[]>([]);
  const [compose, setCompose] = useState<string>('');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [currentProvider, setCurrentProvider] = useState<string>('');
  const [showSaveModal, setShowSaveModal] = useState(false);
  const [saveName, setSaveName] = useState('');
  const [selectedResource, setSelectedResource] = useState<Resource | null>(null);

  // Source selection
  const [sourceType, setSourceType] = useState<SourceType>('api');
  const [provider, setProvider] = useState<Provider>('aws');

  // Saved discoveries query
  const { data: savedDiscoveries = [] } = useQuery({
    queryKey: ['discoveries'],
    queryFn: listDiscoveries,
  });

  // AWS credentials
  const [awsAccessKey, setAwsAccessKey] = useState('');
  const [awsSecretKey, setAwsSecretKey] = useState('');

  // GCP credentials
  const [gcpProjectId, setGcpProjectId] = useState('');
  const [gcpServiceAccount, setGcpServiceAccount] = useState('');

  // Azure credentials
  const [azureSubscriptionId, setAzureSubscriptionId] = useState('');
  const [azureTenantId, setAzureTenantId] = useState('');
  const [azureClientId, setAzureClientId] = useState('');
  const [azureClientSecret, setAzureClientSecret] = useState('');

  useEffect(() => {
    if (resources.length > 0) {
      setSelected(new Set(resources.map(r => r.id)));
    }
  }, [resources]);

  const handleToggle = (id: string) => {
    setSelected(prev => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const handleSelectAll = () => {
    setSelected(new Set(resources.map(r => r.id)));
  };

  const handleSelectNone = () => {
    setSelected(new Set());
  };

  const analyzeMutation = useMutation({
    mutationFn: async (files: File[]) => {
      if (!files || files.length === 0) {
        throw new Error('No files provided');
      }
      const file = files[0];
      const content = await file.text();
      const type = file.name.endsWith('.tf') ? 'terraform'
        : file.name.endsWith('.json') ? 'arm'
        : 'cloudformation';
      return analyzeFiles(type, content);
    },
    onSuccess: (data) => {
      setResources(data.resources);
      setStep('review');
    },
  });

  const discoverMutation = useMutation({
    mutationFn: async () => {
      const request: DiscoverRequest = { provider };

      if (provider === 'aws') {
        request.access_key_id = awsAccessKey;
        request.secret_access_key = awsSecretKey;
        // No region - discover all regions
      } else if (provider === 'gcp') {
        request.project_id = gcpProjectId;
        request.service_account_json = gcpServiceAccount;
      } else if (provider === 'azure') {
        request.subscription_id = azureSubscriptionId;
        request.tenant_id = azureTenantId;
        request.client_id = azureClientId;
        request.client_secret = azureClientSecret;
      }

      return discoverInfrastructure(request);
    },
    onSuccess: (data) => {
      setResources(data.resources);
      setCurrentProvider(data.provider);
      setStep('review');
    },
  });

  // Load saved discovery
  const loadDiscoveryMutation = useMutation({
    mutationFn: getDiscovery,
    onSuccess: (data) => {
      if (data.resources) {
        setResources(data.resources);
        setCurrentProvider(data.provider);
        setStep('review');
      }
    },
  });

  // Save discovery
  const saveDiscoveryMutation = useMutation({
    mutationFn: ({ name, discovery }: { name: string; discovery: AnalyzeResponse }) =>
      saveDiscovery(name, discovery),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['discoveries'] });
      setShowSaveModal(false);
      setSaveName('');
    },
  });

  // Delete discovery
  const deleteDiscoveryMutation = useMutation({
    mutationFn: deleteDiscovery,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['discoveries'] });
    },
  });

  // Deployment store
  const deploymentStore = useDeploymentStore();
  const [scripts, setScripts] = useState<Record<string, string>>({});

  const generateMutation = useMutation({
    mutationFn: ({ resources, options }: { resources: Resource[]; options: { ha: boolean } }) =>
      generateStack(resources, options),
    onSuccess: (data) => {
      setCompose(data.compose);
      setScripts(data.scripts || {});
      setStep('migration-config');
    },
  });

  const [isStartingDeployment, setIsStartingDeployment] = useState(false);

  const handleTargetSelect = (target: DeployTarget | 'export') => {
    if (target === 'export') {
      // Export ZIP fallback
      downloadMutation.mutate();
    } else {
      deploymentStore.setTarget(target);

      // Extract Lambda functions from selected resources
      const lambdaFunctions: Record<string, string> = {};
      resources
        .filter(r => selected.has(r.id) && r.type === 'aws_lambda_function' && r.arn)
        .forEach(r => {
          // Use ARN for Lambda code download
          lambdaFunctions[r.arn!] = r.name;
        });

      // Set compose content and AWS credentials for deployment
      if (target === 'local') {
        deploymentStore.updateLocalConfig({
          composeContent: compose,
          scripts,
          // Pass AWS credentials for Lambda code download (region determined from ARN)
          awsAccessKeyId: awsAccessKey || undefined,
          awsSecretAccessKey: awsSecretKey || undefined,
          lambdaFunctions: Object.keys(lambdaFunctions).length > 0 ? lambdaFunctions : undefined,
        });
      } else {
        deploymentStore.updateSSHConfig({ composeContent: compose, scripts });
      }
      setStep('configure');
    }
  };

  const handleDeploy = async () => {
    setIsStartingDeployment(true);
    try {
      const config = deploymentStore.getConfig();
      if (!config || !deploymentStore.target) return;

      const response = await startDeployment(deploymentStore.target, config);
      deploymentStore.setDeploymentId(response.deployment_id);
      deploymentStore.setStatus('deploying');
      setStep('deploy');
    } catch (err) {
      console.error('Failed to start deployment:', err);
    } finally {
      setIsStartingDeployment(false);
    }
  };

  const handleDeploymentComplete = () => {
    deploymentStore.reset();
    setStep('upload');
    setResources([]);
    setSelected(new Set());
    setCompose('');
  };

  const handleDeploymentRetry = async () => {
    // Reset progress and retry
    deploymentStore.setStatus('configuring');
    setStep('configure');
  };

  const downloadMutation = useMutation({
    mutationFn: () => downloadStack(resources.filter(r => selected.has(r.id)), { ha: false }),
    onSuccess: (blob) => {
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'agnostech-stack.zip';
      a.click();
      // Delay cleanup to ensure download starts
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    },
  });

  return (
    <div className="max-w-6xl mx-auto space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Migration Wizard</h1>
        <div className="flex gap-2 text-sm">
          <span className={step === 'upload' ? 'font-bold' : 'text-muted-foreground'}>1. Upload</span>
          <span className="text-muted-foreground">-&gt;</span>
          <span className={step === 'review' ? 'font-bold' : 'text-muted-foreground'}>2. Review</span>
          <span className="text-muted-foreground">-&gt;</span>
          <span className={step === 'migration-config' ? 'font-bold' : 'text-muted-foreground'}>3. Data Migration</span>
          <span className="text-muted-foreground">-&gt;</span>
          <span className={step === 'target' ? 'font-bold' : 'text-muted-foreground'}>4. Target</span>
          <span className="text-muted-foreground">-&gt;</span>
          <span className={step === 'configure' ? 'font-bold' : 'text-muted-foreground'}>5. Configure</span>
          <span className="text-muted-foreground">-&gt;</span>
          <span className={step === 'deploy' ? 'font-bold' : 'text-muted-foreground'}>6. Deploy</span>
        </div>
      </div>

      {step === 'upload' && (
        <div className="space-y-6">
          {/* Saved discoveries section */}
          {savedDiscoveries.length > 0 && (
            <div className="p-4 bg-gray-50 rounded-lg border">
              <div className="flex items-center gap-2 mb-3">
                <History className="h-5 w-5 text-gray-600" />
                <h3 className="font-medium">Saved Discoveries</h3>
              </div>
              <div className="space-y-2">
                {savedDiscoveries.map((discovery) => (
                  <div
                    key={discovery.id}
                    className="flex items-center justify-between p-3 bg-white rounded-lg border hover:border-gray-300 transition-colors"
                  >
                    <div className="flex items-center gap-3">
                      <span className="px-2 py-0.5 bg-gray-100 text-gray-700 rounded text-xs font-medium uppercase">
                        {discovery.provider}
                      </span>
                      <div>
                        <div className="font-medium">{discovery.name}</div>
                        <div className="text-sm text-muted-foreground">
                          {discovery.resource_count} resources &bull;{' '}
                          {new Date(discovery.created_at).toLocaleDateString()}
                        </div>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() => loadDiscoveryMutation.mutate(discovery.id)}
                        disabled={loadDiscoveryMutation.isPending}
                        className="p-2 text-emerald-600 hover:bg-emerald-50 rounded-lg transition-colors"
                        title="Load discovery"
                      >
                        <FolderOpen className="h-4 w-4" />
                      </button>
                      <button
                        onClick={() => {
                          if (confirm('Delete this saved discovery?')) {
                            deleteDiscoveryMutation.mutate(discovery.id);
                          }
                        }}
                        disabled={deleteDiscoveryMutation.isPending}
                        className="p-2 text-red-600 hover:bg-red-50 rounded-lg transition-colors"
                        title="Delete discovery"
                      >
                        <Trash2 className="h-4 w-4" />
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Source type selector */}
          <div className="flex gap-4">
            <button
              onClick={() => setSourceType('api')}
              className={`flex-1 p-4 rounded-lg border-2 transition-all ${
                sourceType === 'api'
                  ? 'border-emerald-500 bg-emerald-50'
                  : 'border-gray-200 hover:border-gray-300'
              }`}
            >
              <Cloud className="h-8 w-8 mx-auto mb-2 text-emerald-600" />
              <div className="font-medium">Cloud API</div>
              <div className="text-sm text-muted-foreground">Discover live infrastructure</div>
            </button>
            <button
              onClick={() => setSourceType('file')}
              className={`flex-1 p-4 rounded-lg border-2 transition-all ${
                sourceType === 'file'
                  ? 'border-emerald-500 bg-emerald-50'
                  : 'border-gray-200 hover:border-gray-300'
              }`}
            >
              <Upload className="h-8 w-8 mx-auto mb-2 text-blue-600" />
              <div className="font-medium">Upload Files</div>
              <div className="text-sm text-muted-foreground">Terraform, CloudFormation, ARM</div>
            </button>
          </div>

          {/* File upload */}
          {sourceType === 'file' && (
            <div className="space-y-4">
              <FileDropzone onFilesAccepted={(files) => analyzeMutation.mutate(files)} />
              {analyzeMutation.isPending && (
                <div className="flex items-center gap-2 text-muted-foreground">
                  <Loader2 className="h-4 w-4 animate-spin" />
                  Analyzing files...
                </div>
              )}
              {analyzeMutation.isError && (
                <p className="text-red-500">Error: {analyzeMutation.error.message}</p>
              )}
            </div>
          )}

          {/* Cloud API discovery */}
          {sourceType === 'api' && (
            <div className="space-y-4">
              {/* Provider selector */}
              <div className="flex gap-2">
                {(['aws', 'gcp', 'azure'] as Provider[]).map((p) => (
                  <button
                    key={p}
                    onClick={() => setProvider(p)}
                    className={`px-4 py-2 rounded-lg font-medium uppercase text-sm ${
                      provider === p
                        ? 'bg-gray-900 text-white'
                        : 'bg-gray-100 text-gray-600 hover:bg-gray-200'
                    }`}
                  >
                    {p}
                  </button>
                ))}
              </div>

              {/* AWS credentials */}
              {provider === 'aws' && (
                <div className="grid gap-4 p-4 bg-gray-50 rounded-lg">
                  <div>
                    <label className="block text-sm font-medium mb-1">Access Key ID</label>
                    <input
                      type="text"
                      value={awsAccessKey}
                      onChange={(e) => setAwsAccessKey(e.target.value)}
                      className="w-full px-3 py-2 border rounded-lg"
                      placeholder="AKIA..."
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1">Secret Access Key</label>
                    <input
                      type="password"
                      value={awsSecretKey}
                      onChange={(e) => setAwsSecretKey(e.target.value)}
                      className="w-full px-3 py-2 border rounded-lg"
                      placeholder="••••••••"
                    />
                  </div>
                  <p className="text-sm text-muted-foreground">
                    All enabled regions will be scanned automatically.
                  </p>
                </div>
              )}

              {/* GCP credentials */}
              {provider === 'gcp' && (
                <div className="grid gap-4 p-4 bg-gray-50 rounded-lg">
                  <div>
                    <label className="block text-sm font-medium mb-1">Project ID</label>
                    <input
                      type="text"
                      value={gcpProjectId}
                      onChange={(e) => setGcpProjectId(e.target.value)}
                      className="w-full px-3 py-2 border rounded-lg"
                      placeholder="my-project-123"
                    />
                  </div>
                  <div>
                    <label className="block text-sm font-medium mb-1">Service Account JSON</label>
                    <textarea
                      value={gcpServiceAccount}
                      onChange={(e) => setGcpServiceAccount(e.target.value)}
                      className="w-full px-3 py-2 border rounded-lg font-mono text-sm"
                      rows={4}
                      placeholder='{"type": "service_account", ...}'
                    />
                  </div>
                </div>
              )}

              {/* Azure credentials */}
              {provider === 'azure' && (
                <div className="grid gap-4 p-4 bg-gray-50 rounded-lg">
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <label className="block text-sm font-medium mb-1">Subscription ID</label>
                      <input
                        type="text"
                        value={azureSubscriptionId}
                        onChange={(e) => setAzureSubscriptionId(e.target.value)}
                        className="w-full px-3 py-2 border rounded-lg"
                      />
                    </div>
                    <div>
                      <label className="block text-sm font-medium mb-1">Tenant ID</label>
                      <input
                        type="text"
                        value={azureTenantId}
                        onChange={(e) => setAzureTenantId(e.target.value)}
                        className="w-full px-3 py-2 border rounded-lg"
                      />
                    </div>
                  </div>
                  <div className="grid grid-cols-2 gap-4">
                    <div>
                      <label className="block text-sm font-medium mb-1">Client ID</label>
                      <input
                        type="text"
                        value={azureClientId}
                        onChange={(e) => setAzureClientId(e.target.value)}
                        className="w-full px-3 py-2 border rounded-lg"
                      />
                    </div>
                    <div>
                      <label className="block text-sm font-medium mb-1">Client Secret</label>
                      <input
                        type="password"
                        value={azureClientSecret}
                        onChange={(e) => setAzureClientSecret(e.target.value)}
                        className="w-full px-3 py-2 border rounded-lg"
                      />
                    </div>
                  </div>
                </div>
              )}

              {/* Discover button */}
              <button
                onClick={() => discoverMutation.mutate()}
                disabled={discoverMutation.isPending}
                className="w-full py-3 bg-emerald-600 text-white rounded-lg font-medium hover:bg-emerald-700 disabled:opacity-50 flex items-center justify-center gap-2"
              >
                {discoverMutation.isPending ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin" />
                    Discovering infrastructure...
                  </>
                ) : (
                  <>
                    <Cloud className="h-4 w-4" />
                    Discover Infrastructure
                  </>
                )}
              </button>

              {discoverMutation.isError && (
                <p className="text-red-500">Error: {discoverMutation.error.message}</p>
              )}

              <div className="flex items-center gap-2 p-3 bg-blue-50 border border-blue-200 rounded-lg text-blue-800">
                <svg className="h-5 w-5 flex-shrink-0" fill="currentColor" viewBox="0 0 20 20">
                  <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z" clipRule="evenodd" />
                </svg>
                <p className="text-sm">
                  <strong>Read-only access.</strong> Credentials are used only for discovery and are never stored.
                </p>
              </div>
            </div>
          )}
        </div>
      )}

      {step === 'review' && (
        <div className="fixed inset-0 z-40 flex flex-col bg-slate-50">
          {/* Top bar */}
          <div className="flex items-center justify-between px-6 py-3 bg-white border-b shadow-sm">
            <div className="flex items-center gap-4">
              <button
                onClick={() => {
                  setStep('upload');
                  setResources([]);
                  setSelected(new Set());
                }}
                className="flex items-center gap-2 px-3 py-1.5 text-gray-600 hover:text-gray-900 hover:bg-gray-100 rounded-lg transition-colors"
              >
                <ArrowLeft className="h-4 w-4" />
                Back
              </button>
              <div className="h-6 w-px bg-gray-200" />
              <h2 className="text-lg font-semibold">Architecture Review</h2>
            </div>
            <div className="flex items-center gap-3">
              <button
                onClick={() => setShowSaveModal(true)}
                className="flex items-center gap-2 px-3 py-1.5 text-gray-600 hover:text-gray-900 hover:bg-gray-100 rounded-lg transition-colors"
              >
                <Save className="h-4 w-4" />
                Save
              </button>
              <button
                onClick={() => {
                  const selectedResources = resources.filter(r => selected.has(r.id));
                  generateMutation.mutate({ resources: selectedResources, options: { ha: false } });
                }}
                disabled={selected.size === 0 || generateMutation.isPending}
                className="flex items-center gap-2 px-4 py-2 bg-emerald-600 text-white rounded-lg hover:bg-emerald-700 disabled:opacity-50 disabled:cursor-not-allowed font-medium"
              >
                {generateMutation.isPending ? (
                  <>
                    <Loader2 className="h-4 w-4 animate-spin" />
                    Generating...
                  </>
                ) : (
                  `Generate Stack (${selected.size})`
                )}
              </button>
            </div>
          </div>

          {/* Detail panel - shown above diagram when resource selected */}
          {selectedResource && (
            <ResourceDetailPanel
              resource={selectedResource}
              onClose={() => setSelectedResource(null)}
            />
          )}

          {/* Main content */}
          <div className="flex-1 flex overflow-hidden">
            {/* Diagram - fullscreen */}
            <div className="flex-1">
              <ArchitectureDiagram
                resources={resources}
                selected={selected}
                onToggle={handleToggle}
                onSelect={setSelectedResource}
              />
            </div>
            {/* Floating summary panel */}
            <div className="w-80 bg-white border-l shadow-lg overflow-y-auto">
              <SelectionSummary
                resources={resources}
                selected={selected}
                onSelectAll={handleSelectAll}
                onSelectNone={handleSelectNone}
              />
            </div>
          </div>
        </div>
      )}

      {/* Save discovery modal */}
      {showSaveModal && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-white rounded-lg p-6 w-full max-w-md shadow-xl">
            <h3 className="text-lg font-semibold mb-4">Save Discovery</h3>
            <p className="text-sm text-muted-foreground mb-4">
              Save this discovery to load it later without re-entering credentials.
            </p>
            <input
              type="text"
              value={saveName}
              onChange={(e) => setSaveName(e.target.value)}
              placeholder="Discovery name (e.g., Production AWS)"
              className="w-full px-3 py-2 border rounded-lg mb-4"
              autoFocus
            />
            <div className="flex justify-end gap-3">
              <button
                onClick={() => {
                  setShowSaveModal(false);
                  setSaveName('');
                }}
                className="px-4 py-2 text-gray-600 hover:text-gray-900"
              >
                Cancel
              </button>
              <button
                onClick={() => {
                  if (saveName.trim()) {
                    saveDiscoveryMutation.mutate({
                      name: saveName.trim(),
                      discovery: {
                        resources,
                        warnings: [],
                        provider: currentProvider,
                      },
                    });
                  }
                }}
                disabled={!saveName.trim() || saveDiscoveryMutation.isPending}
                className="px-4 py-2 bg-emerald-600 text-white rounded-lg hover:bg-emerald-700 disabled:opacity-50"
              >
                {saveDiscoveryMutation.isPending ? 'Saving...' : 'Save'}
              </button>
            </div>
          </div>
        </div>
      )}

      {step === 'migration-config' && (
        <MigrationConfigStep
          onBack={() => setStep('review')}
          onNext={() => setStep('target')}
          resources={resources.filter(r => selected.has(r.id))}
        />
      )}

      {step === 'target' && (
        <div className="max-w-2xl mx-auto">
          <div className="mb-6">
            <Button variant="outline" onClick={() => setStep('migration-config')}>
              <ArrowLeft className="h-4 w-4 mr-2" />
              Back to Data Migration
            </Button>
          </div>
          <TargetSelector onSelect={handleTargetSelect} />
        </div>
      )}

      {step === 'configure' && (
        <div className="max-w-2xl mx-auto">
          <ConfigurationForm
            onBack={() => {
              deploymentStore.setTarget(null);
              setStep('target');
            }}
            onDeploy={handleDeploy}
            isDeploying={isStartingDeployment}
          />
        </div>
      )}

      {step === 'deploy' && (
        <div className="max-w-4xl mx-auto">
          <DeploymentExecution
            onRetry={handleDeploymentRetry}
            onComplete={handleDeploymentComplete}
          />
        </div>
      )}
    </div>
  );
}
