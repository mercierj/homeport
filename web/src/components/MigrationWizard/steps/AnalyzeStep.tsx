import { useState, useCallback } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Cloud,
  FileCode,
  Database,
  Search,
  ArrowLeft,
  Save,
  History,
  FolderOpen,
  Trash2,
  Layers,
  Info,
  Loader2,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { buttonVariants } from '@/lib/button-variants';
import { useWizardStore } from '@/stores/wizard';
import {
  analyzeFiles,
  discoverInfrastructureWithProgress,
  listDiscoveries,
  getDiscovery,
  saveDiscovery,
  deleteDiscovery,
  type DiscoverRequest,
  type Resource,
} from '@/lib/migrate-api';
import { FileDropzone } from '@/components/FileDropzone';
import { ArchitectureDiagram } from '@/components/ArchitectureDiagram';
import { ResourceDetailPanel } from '@/components/ResourceDetailPanel';
import { SelectionSummary } from '@/components/SelectionSummary';
import { ConsolidationPreviewPanel } from '../ConsolidationPreviewPanel';

// Internal phase for AnalyzeStep
type AnalyzePhase = 'source' | 'discovery' | 'review';

// Source type options
const SOURCE_TYPES = [
  {
    id: 'terraform',
    label: 'Terraform Files',
    description: 'Parse .tf files to discover resources',
    icon: FileCode,
    provider: null,
  },
  {
    id: 'tfstate',
    label: 'Terraform State',
    description: 'Read terraform.tfstate for exact state',
    icon: Database,
    provider: null,
  },
  {
    id: 'cloudformation',
    label: 'CloudFormation',
    description: 'Parse AWS CloudFormation YAML/JSON',
    icon: Cloud,
    provider: 'aws' as const,
  },
  {
    id: 'arm',
    label: 'ARM Templates',
    description: 'Parse Azure Resource Manager templates',
    icon: Cloud,
    provider: 'azure' as const,
  },
  {
    id: 'aws-api',
    label: 'AWS Live Scan',
    description: 'Discover resources directly from AWS API',
    icon: Cloud,
    provider: 'aws' as const,
  },
  {
    id: 'gcp-api',
    label: 'GCP Live Scan',
    description: 'Discover resources from Google Cloud API',
    icon: Cloud,
    provider: 'gcp' as const,
  },
  {
    id: 'azure-api',
    label: 'Azure Live Scan',
    description: 'Discover resources from Azure API',
    icon: Cloud,
    provider: 'azure' as const,
  },
] as const;

export function AnalyzeStep() {
  const queryClient = useQueryClient();

  const {
    sourceType,
    sourcePath,
    sourceProvider,
    analysisResult,
    selectedResources,
    isAnalyzing,
    awsCredentials,
    gcpCredentials,
    azureCredentials,
    discoveryProgress,
    isDiscovering,
    discoveryError,
    consolidate,
    selectedResourceForDetail,
    setSourceType,
    setSourcePath,
    setSourceProvider,
    setAnalysisResult,
    setSelectedResources,
    setIsAnalyzing,
    setAwsCredentials,
    setGcpCredentials,
    setAzureCredentials,
    setDiscoveryProgress,
    setIsDiscovering,
    setDiscoveryError,
    setConsolidate,
    setSelectedResourceForDetail,
    setSavedDiscoveryId,
    setError,
    nextStep,
  } = useWizardStore();

  // Internal phase state
  const [phase, setPhase] = useState<AnalyzePhase>(
    analysisResult ? 'review' : 'source'
  );
  const [fileContent, setFileContent] = useState<string>('');
  const [showSaveModal, setShowSaveModal] = useState(false);
  const [saveName, setSaveName] = useState('');

  // Saved discoveries query
  const { data: savedDiscoveries = [] } = useQuery({
    queryKey: ['discoveries'],
    queryFn: listDiscoveries,
  });

  // Load saved discovery
  const loadDiscoveryMutation = useMutation({
    mutationFn: getDiscovery,
    onSuccess: (data) => {
      if (data.resources) {
        setAnalysisResult({
          resources: data.resources,
          warnings: [],
          provider: data.provider,
        });
        setSourceProvider(data.provider as 'aws' | 'gcp' | 'azure');
        setSavedDiscoveryId(data.id);
        setPhase('review');
      }
    },
  });

  // Save discovery mutation
  const saveDiscoveryMutation = useMutation({
    mutationFn: ({ name, discovery }: { name: string; discovery: { resources: Resource[]; warnings: string[]; provider: string } }) =>
      saveDiscovery(name, discovery),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['discoveries'] });
      setShowSaveModal(false);
      setSaveName('');
    },
  });

  // Delete discovery mutation
  const deleteDiscoveryMutation = useMutation({
    mutationFn: deleteDiscovery,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['discoveries'] });
    },
  });

  // Handle file drop
  const handleFileDrop = useCallback((files: File[]) => {
    if (files.length > 0) {
      const file = files[0];
      setSourcePath(file.name);
      const reader = new FileReader();
      reader.onload = (e) => {
        setFileContent(e.target?.result as string);
      };
      reader.readAsText(file);
    }
  }, [setSourcePath]);

  // Handle analyze
  const handleAnalyze = async () => {
    setIsAnalyzing(true);
    setIsDiscovering(true);
    setDiscoveryError(null);
    setError(null);

    try {
      let result;

      if (sourceType?.endsWith('-api')) {
        // Live API discovery
        const provider = sourceType.replace('-api', '') as 'aws' | 'gcp' | 'azure';
        setSourceProvider(provider);

        const request: DiscoverRequest = { provider };

        if (provider === 'aws') {
          request.access_key_id = awsCredentials.accessKeyId;
          request.secret_access_key = awsCredentials.secretAccessKey;
        } else if (provider === 'gcp') {
          request.project_id = gcpCredentials.projectId;
          request.service_account_json = gcpCredentials.serviceAccountJson;
        } else if (provider === 'azure') {
          request.subscription_id = azureCredentials.subscriptionId;
          request.tenant_id = azureCredentials.tenantId;
          request.client_id = azureCredentials.clientId;
          request.client_secret = azureCredentials.clientSecret;
        }

        result = await discoverInfrastructureWithProgress(request, setDiscoveryProgress);
      } else {
        // File-based analysis
        result = await analyzeFiles(sourceType || 'terraform', fileContent);
      }

      setAnalysisResult(result);
      setPhase('review');
    } catch (err) {
      const errorMessage = err instanceof Error ? err.message : 'Analysis failed';
      setDiscoveryError(errorMessage);
      setError(errorMessage);
    } finally {
      setIsAnalyzing(false);
      setIsDiscovering(false);
      setDiscoveryProgress(null);
    }
  };

  // Toggle resource selection (for diagram)
  const handleToggle = (id: string) => {
    const isSelected = selectedResources.some((r) => r.id === id);
    if (isSelected) {
      setSelectedResources(selectedResources.filter((r) => r.id !== id));
    } else {
      const resource = analysisResult?.resources.find((r) => r.id === id);
      if (resource) {
        setSelectedResources([...selectedResources, resource]);
      }
    }
  };

  // Select/deselect all
  const handleSelectAll = () => {
    setSelectedResources(analysisResult?.resources || []);
  };

  const handleSelectNone = () => {
    setSelectedResources([]);
  };

  // Convert selectedResources to Set<string> for ArchitectureDiagram
  const selectedIds = new Set(selectedResources.map((r) => r.id));

  // Check if API source requires credentials
  const isApiSource = sourceType?.endsWith('-api');
  const needsCredentials = isApiSource && !analysisResult;

  // Can proceed to next step
  const canProceed = analysisResult && selectedResources.length > 0;

  // ============== REVIEW PHASE (Full-screen Architecture Diagram) ==============
  if (phase === 'review' && analysisResult) {
    return (
      <div className="fixed inset-0 z-40 flex flex-col bg-slate-50 dark:bg-background">
        {/* Top bar */}
        <div className="flex items-center justify-between px-6 py-3 bg-white dark:bg-card border-b shadow-sm">
          <div className="flex items-center gap-4">
            <button
              onClick={() => {
                setPhase('source');
                setAnalysisResult(null);
                setSelectedResources([]);
                setSelectedResourceForDetail(null);
              }}
              className="flex items-center gap-2 px-3 py-1.5 text-muted-foreground hover:text-foreground hover:bg-muted rounded-lg transition-colors"
            >
              <ArrowLeft className="h-4 w-4" />
              Back
            </button>
            <div className="h-6 w-px bg-muted" />
            <h2 className="text-lg font-semibold">Architecture Review</h2>
          </div>
          <div className="flex items-center gap-3">
            <button
              onClick={() => setShowSaveModal(true)}
              className="flex items-center gap-2 px-3 py-1.5 text-muted-foreground hover:text-foreground hover:bg-muted rounded-lg transition-colors"
            >
              <Save className="h-4 w-4" />
              Save
            </button>
            {/* Stack Consolidation Toggle */}
            <div className="flex items-center gap-2 px-3 py-1.5 border rounded-lg bg-muted/50">
              <button
                onClick={() => setConsolidate(!consolidate)}
                className={cn(
                  'flex items-center gap-2 px-2 py-1 rounded transition-colors',
                  consolidate
                    ? 'bg-accent text-white'
                    : 'text-muted-foreground hover:text-foreground'
                )}
                title="Consolidate similar resources into unified stacks to reduce container count"
              >
                <Layers className="h-4 w-4" />
                <span className="text-sm font-medium">Consolidate</span>
              </button>
              <div className="relative group">
                <Info className="h-4 w-4 text-muted-foreground cursor-help" />
                <div className="absolute right-0 top-6 w-64 p-3 bg-white dark:bg-card border rounded-lg shadow-lg opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all z-50">
                  <p className="text-xs text-muted-foreground">
                    <strong className="text-foreground">Stack Consolidation</strong> groups similar resources
                    (e.g., 3 RDS instances, 5 SQS queues) into unified stacks (1 PostgreSQL, 1 RabbitMQ),
                    reducing container sprawl.
                  </p>
                </div>
              </div>
            </div>
            <button
              onClick={nextStep}
              disabled={!canProceed}
              className={cn(
                'flex items-center gap-2 px-4 py-2 bg-accent text-white rounded-lg hover:bg-accent/90 disabled:opacity-50 disabled:cursor-not-allowed font-medium'
              )}
            >
              Continue to Export ({selectedResources.length})
            </button>
          </div>
        </div>

        {/* Detail panel - shown above diagram when resource selected */}
        {selectedResourceForDetail && (
          <ResourceDetailPanel
            resource={selectedResourceForDetail}
            onClose={() => setSelectedResourceForDetail(null)}
          />
        )}

        {/* Main content */}
        <div className="flex-1 flex overflow-hidden">
          {/* Diagram - fullscreen */}
          <div className="flex-1">
            <ArchitectureDiagram
              resources={analysisResult.resources}
              selected={selectedIds}
              onToggle={handleToggle}
              onSelect={setSelectedResourceForDetail}
            />
          </div>
          {/* Floating summary panel */}
          <div className="w-80 bg-white dark:bg-card border-l shadow-lg flex flex-col">
            {/* Consolidation Preview - fixed at top */}
            {consolidate && selectedResources.length > 0 && (
              <div className="flex-shrink-0">
                <ConsolidationPreviewPanel resources={selectedResources} />
              </div>
            )}

            {/* Selection Summary - scrollable */}
            <div className="flex-1 overflow-y-auto">
              <SelectionSummary
                resources={analysisResult.resources}
                selected={selectedIds}
                onSelectAll={handleSelectAll}
                onSelectNone={handleSelectNone}
              />
            </div>
          </div>
        </div>

        {/* Save discovery modal */}
        {showSaveModal && (
          <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
            <div className="bg-white dark:bg-card rounded-lg p-6 w-full max-w-md shadow-xl">
              <h3 className="text-lg font-semibold mb-4">Save Discovery</h3>
              <p className="text-sm text-muted-foreground mb-4">
                Save this discovery to load it later without re-entering credentials.
              </p>
              <input
                type="text"
                value={saveName}
                onChange={(e) => setSaveName(e.target.value)}
                placeholder="Discovery name (e.g., Production AWS)"
                className="input w-full mb-4"
                autoFocus
              />
              <div className="flex justify-end gap-3">
                <button
                  onClick={() => {
                    setShowSaveModal(false);
                    setSaveName('');
                  }}
                  className="px-4 py-2 text-muted-foreground hover:text-foreground"
                >
                  Cancel
                </button>
                <button
                  onClick={() => {
                    if (saveName.trim() && analysisResult) {
                      saveDiscoveryMutation.mutate({
                        name: saveName.trim(),
                        discovery: {
                          resources: analysisResult.resources,
                          warnings: analysisResult.warnings || [],
                          provider: sourceProvider || '',
                        },
                      });
                    }
                  }}
                  disabled={!saveName.trim() || saveDiscoveryMutation.isPending}
                  className="px-4 py-2 bg-accent text-white rounded-lg hover:bg-accent/90 disabled:opacity-50"
                >
                  {saveDiscoveryMutation.isPending ? 'Saving...' : 'Save'}
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    );
  }

  // ============== SOURCE PHASE ==============
  return (
    <div className="space-y-6">
      {/* Saved discoveries section */}
      {savedDiscoveries.length > 0 && (
        <div className="p-4 bg-muted rounded-lg border">
          <div className="flex items-center gap-2 mb-3">
            <History className="h-5 w-5 text-muted-foreground" />
            <h3 className="font-medium">Saved Discoveries</h3>
          </div>
          <div className="space-y-2">
            {savedDiscoveries.map((discovery) => (
              <div
                key={discovery.id}
                className="flex items-center justify-between p-3 bg-white dark:bg-card rounded-lg border hover:border-muted-foreground/50 transition-colors"
              >
                <div className="flex items-center gap-3">
                  <span className="px-2 py-0.5 bg-muted text-muted-foreground rounded text-xs font-medium uppercase">
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
                    className="p-2 text-accent hover:bg-accent/10 rounded-lg transition-colors"
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
                    className="p-2 text-error hover:bg-error/10 rounded-lg transition-colors"
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

      {/* Source type selection */}
      <div>
        <h3 className="text-lg font-semibold mb-4">Select Source Type</h3>
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {SOURCE_TYPES.map((source) => (
            <button
              key={source.id}
              onClick={() => {
                setSourceType(source.id as typeof sourceType);
                if (source.provider) {
                  setSourceProvider(source.provider);
                }
              }}
              className={cn(
                'card-action p-4 text-left',
                sourceType === source.id && 'card-action-active border-primary'
              )}
            >
              <div className="flex items-start gap-3">
                <div className={cn(
                  'p-2 rounded-lg',
                  source.provider === 'aws' && 'bg-cloud-aws/10 text-cloud-aws',
                  source.provider === 'gcp' && 'bg-cloud-gcp/10 text-cloud-gcp',
                  source.provider === 'azure' && 'bg-cloud-azure/10 text-cloud-azure',
                  !source.provider && 'bg-primary/10 text-primary'
                )}>
                  <source.icon className="w-5 h-5" />
                </div>
                <div>
                  <p className="font-medium">{source.label}</p>
                  <p className="text-sm text-muted-foreground">
                    {source.description}
                  </p>
                </div>
              </div>
            </button>
          ))}
        </div>
      </div>

      {/* File upload for non-API sources */}
      {sourceType && !isApiSource && (
        <div>
          <h3 className="text-lg font-semibold mb-4">Upload Files</h3>
          <FileDropzone
            onFilesAccepted={handleFileDrop}
            accept={{
              'text/*': ['.tf', '.tfstate', '.json', '.yaml', '.yml'],
            }}
          />
          {sourcePath && (
            <p className="mt-2 text-sm text-muted-foreground">
              Selected: <span className="font-medium">{sourcePath}</span>
            </p>
          )}
        </div>
      )}

      {/* AWS credentials */}
      {needsCredentials && sourceType === 'aws-api' && (
        <div className="space-y-4">
          <h3 className="text-lg font-semibold">AWS Credentials</h3>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="label">Access Key ID</label>
              <input
                type="text"
                value={awsCredentials.accessKeyId}
                onChange={(e) => setAwsCredentials({ accessKeyId: e.target.value })}
                className="input"
                placeholder="AKIA..."
              />
            </div>
            <div>
              <label className="label">Secret Access Key</label>
              <input
                type="password"
                value={awsCredentials.secretAccessKey}
                onChange={(e) => setAwsCredentials({ secretAccessKey: e.target.value })}
                className="input"
                placeholder="Secret key"
              />
            </div>
          </div>
          <p className="text-sm text-muted-foreground">
            All enabled regions will be scanned automatically.
          </p>
        </div>
      )}

      {/* GCP credentials */}
      {needsCredentials && sourceType === 'gcp-api' && (
        <div className="space-y-4">
          <h3 className="text-lg font-semibold">GCP Credentials</h3>
          <div className="grid grid-cols-1 gap-4">
            <div>
              <label className="label">Project ID</label>
              <input
                type="text"
                value={gcpCredentials.projectId}
                onChange={(e) => setGcpCredentials({ projectId: e.target.value })}
                className="input"
                placeholder="my-project-123"
              />
            </div>
            <div>
              <label className="label">Service Account JSON</label>
              <textarea
                value={gcpCredentials.serviceAccountJson}
                onChange={(e) => setGcpCredentials({ serviceAccountJson: e.target.value })}
                className="textarea min-h-[100px]"
                placeholder='{"type": "service_account", ...}'
              />
            </div>
          </div>
        </div>
      )}

      {/* Azure credentials */}
      {needsCredentials && sourceType === 'azure-api' && (
        <div className="space-y-4">
          <h3 className="text-lg font-semibold">Azure Credentials</h3>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div>
              <label className="label">Subscription ID</label>
              <input
                type="text"
                value={azureCredentials.subscriptionId}
                onChange={(e) => setAzureCredentials({ subscriptionId: e.target.value })}
                className="input"
                placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
              />
            </div>
            <div>
              <label className="label">Tenant ID</label>
              <input
                type="text"
                value={azureCredentials.tenantId}
                onChange={(e) => setAzureCredentials({ tenantId: e.target.value })}
                className="input"
                placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
              />
            </div>
            <div>
              <label className="label">Client ID</label>
              <input
                type="text"
                value={azureCredentials.clientId}
                onChange={(e) => setAzureCredentials({ clientId: e.target.value })}
                className="input"
                placeholder="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
              />
            </div>
            <div>
              <label className="label">Client Secret</label>
              <input
                type="password"
                value={azureCredentials.clientSecret}
                onChange={(e) => setAzureCredentials({ clientSecret: e.target.value })}
                className="input"
                placeholder="Secret"
              />
            </div>
          </div>
        </div>
      )}

      {/* Analyze button */}
      {sourceType && !isDiscovering && (
        <div className="flex justify-center">
          <button
            onClick={handleAnalyze}
            disabled={isAnalyzing || (!isApiSource && !fileContent)}
            className={cn(
              buttonVariants({ variant: 'primary', size: 'lg' }),
              'gap-2',
              (isAnalyzing || (!isApiSource && !fileContent)) &&
                'opacity-50 cursor-not-allowed'
            )}
          >
            <Search className="w-5 h-5" />
            {isAnalyzing ? 'Analyzing...' : 'Analyze'}
          </button>
        </div>
      )}

      {/* Discovery progress */}
      {isDiscovering && (
        <div className="p-4 bg-muted rounded-lg border space-y-3">
          <div className="flex items-center gap-3">
            <Loader2 className="h-5 w-5 animate-spin text-accent" />
            <div className="flex-1">
              <div className="font-medium text-sm">
                {discoveryProgress?.message || 'Connecting...'}
              </div>
              {discoveryProgress && discoveryProgress.total_regions > 0 && (
                <div className="text-xs text-muted-foreground mt-0.5">
                  Region {discoveryProgress.current_region} of {discoveryProgress.total_regions}
                  {discoveryProgress.service && ` • ${discoveryProgress.service}`}
                </div>
              )}
            </div>
            {discoveryProgress && discoveryProgress.resources_found > 0 && (
              <div className="text-sm font-medium text-accent">
                {discoveryProgress.resources_found} found
              </div>
            )}
          </div>

          {/* Progress bar */}
          {discoveryProgress && discoveryProgress.total_regions > 0 && (
            <div className="progress h-2">
              <div
                className="progress-indicator bg-accent transition-all duration-300"
                style={{
                  width: `${Math.round(
                    ((discoveryProgress.current_region - 1) * discoveryProgress.total_services +
                    discoveryProgress.current_service) /
                    (discoveryProgress.total_regions * discoveryProgress.total_services) * 100
                  )}%`
                }}
              />
            </div>
          )}
        </div>
      )}

      {/* Discovery error */}
      {discoveryError && (
        <div className="alert-error">
          <p>Error: {discoveryError}</p>
        </div>
      )}

      {/* Security notice for API discovery */}
      {isApiSource && (
        <div className="flex items-center gap-2 p-3 bg-info/10 border border-info/30 rounded-lg text-info">
          <svg className="h-5 w-5 flex-shrink-0" fill="currentColor" viewBox="0 0 20 20">
            <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z" clipRule="evenodd" />
          </svg>
          <p className="text-sm">
            <strong>Read-only access.</strong> Credentials are used only for discovery and are never stored.
          </p>
        </div>
      )}
    </div>
  );
}
