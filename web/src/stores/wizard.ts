import { create } from 'zustand';
import type { Resource, AnalyzeResponse, DiscoverProgressEvent } from '@/lib/migrate-api';

// Wizard step definitions
export type WizardStep =
  | 'analyze'    // Step 1a: Analyze source (source entry)
  | 'export'     // Step 2a: Export bundle (source entry)
  | 'upload'     // Step 1b: Upload bundle (bundle entry)
  | 'secrets'    // Step 3: Provide secrets
  | 'deploy'     // Step 4: Deploy to target
  | 'sync'       // Step 5: Sync data
  | 'cutover';   // Step 6: DNS cutover

// Steps for source entry point (analyze -> export -> secrets -> ...)
export const SOURCE_WIZARD_STEPS: WizardStep[] = [
  'analyze',
  'export',
  'secrets',
  'deploy',
  'sync',
  'cutover',
];

// Steps for bundle entry point (upload -> secrets -> ...)
export const BUNDLE_WIZARD_STEPS: WizardStep[] = [
  'upload',
  'secrets',
  'deploy',
  'sync',
  'cutover',
];

// All possible steps (for type safety)
export const WIZARD_STEPS: WizardStep[] = [
  'analyze',
  'export',
  'upload',
  'secrets',
  'deploy',
  'sync',
  'cutover',
];

export const STEP_LABELS: Record<WizardStep, string> = {
  analyze: 'Analyze',
  export: 'Export',
  upload: 'Upload',
  secrets: 'Secrets',
  deploy: 'Deploy',
  sync: 'Sync',
  cutover: 'Cutover',
};

export const STEP_DESCRIPTIONS: Record<WizardStep, string> = {
  analyze: 'Analyze source infrastructure',
  export: 'Create .hprt migration bundle',
  upload: 'Upload migration bundle',
  secrets: 'Provide required secrets',
  deploy: 'Deploy to target server',
  sync: 'Synchronize data',
  cutover: 'DNS cutover & validation',
};

// Bundle manifest types
export interface BundleManifest {
  version: string;
  format: string;
  created: string;
  homeport_version: string;
  source: {
    provider: string;
    region: string;
    account_id?: string;
    resource_count: number;
    analyzed_at: string;
  };
  target: {
    type: string;
    consolidation: boolean;
    stack_count: number;
  };
  stacks: BundleStack[];
  checksums: Record<string, string>;
  data_sync?: {
    total_estimated_size: string;
    databases: string[];
    storage: string[];
    estimated_duration: string;
  };
  rollback: {
    supported: boolean;
    snapshot_required: boolean;
  };
}

export interface BundleStack {
  name: string;
  services: string[];
  resources_consolidated: number;
  data_sync_required: boolean;
  estimated_sync_size?: string;
}

// Secret reference (from bundle)
export interface SecretReference {
  name: string;
  source: 'manual' | 'env' | 'file' | 'aws-secrets-manager' | 'gcp-secret-manager' | 'azure-key-vault' | 'hashicorp-vault';
  key?: string;
  description?: string;
  required: boolean;
}

// Sync task types
export type SyncStatus = 'pending' | 'running' | 'completed' | 'failed' | 'skipped';

export interface SyncTask {
  id: string;
  type: 'database' | 'storage' | 'cache';
  name: string;
  source: string;
  target: string;
  status: SyncStatus;
  progress: number;
  bytesTotal: number;
  bytesTransferred: number;
  itemsTotal: number;
  itemsCompleted: number;
  error?: string;
}

// Deploy target types
export type DeployTarget = 'local' | 'ssh';

export interface SSHConfig {
  host: string;
  port: number;
  username: string;
  authMethod: 'key' | 'password';
  keyPath?: string;
  password?: string;
}

// Entry point types
export type WizardEntryPoint = 'source' | 'bundle' | null;

// Cloud credential types
export interface AWSCredentials {
  accessKeyId: string;
  secretAccessKey: string;
}

export interface GCPCredentials {
  projectId: string;
  serviceAccountJson: string;
}

export interface AzureCredentials {
  subscriptionId: string;
  tenantId: string;
  clientId: string;
  clientSecret: string;
}

// Consolidation preview types
export interface ConsolidationPreview {
  sourceCount: number;
  serviceCount: number;
  reductionRatio: number;
  stacks: Array<{
    type: string;
    displayName: string;
    resourceCount: number;
  }>;
}

// Wizard state
export interface WizardState {
  // Entry point
  entryPoint: WizardEntryPoint;

  // Navigation
  currentStep: WizardStep;
  completedSteps: WizardStep[];

  // Step 1: Analyze
  sourceType: 'terraform' | 'tfstate' | 'cloudformation' | 'arm' | 'aws-api' | 'gcp-api' | 'azure-api' | null;
  sourcePath: string;
  sourceProvider: 'aws' | 'gcp' | 'azure' | null;
  analysisResult: AnalyzeResponse | null;
  selectedResources: Resource[];
  isAnalyzing: boolean;

  // Cloud credentials (for API discovery)
  awsCredentials: AWSCredentials;
  gcpCredentials: GCPCredentials;
  azureCredentials: AzureCredentials;

  // Discovery progress (SSE streaming)
  discoveryProgress: DiscoverProgressEvent | null;
  isDiscovering: boolean;
  discoveryError: string | null;

  // Saved discoveries
  savedDiscoveryId: string | null;

  // Review phase (architecture diagram)
  selectedResourceForDetail: Resource | null;
  consolidationPreview: ConsolidationPreview | null;

  // Step 2: Export
  bundleId: string | null;
  bundleName: string;
  bundleManifest: BundleManifest | null;
  domain: string;
  consolidate: boolean;
  isExporting: boolean;

  // Upload mode (bundle entry point)
  uploadedBundle: File | null;

  // Step 3: Secrets
  secretRefs: SecretReference[];
  secretValues: Record<string, string>;
  secretsResolved: boolean;

  // Step 4: Deploy
  deployTarget: DeployTarget | null;
  sshConfig: SSHConfig;
  deploymentId: string | null;
  deployProgress: number;
  isDeploying: boolean;

  // Step 5: Sync
  syncPlanId: string | null;
  syncTasks: SyncTask[];
  syncProgress: number;
  isSyncing: boolean;

  // Step 6: Cutover
  cutoverPlanId: string | null;
  dnsChanges: Array<{
    domain: string;
    recordType: string;
    oldValue: string;
    newValue: string;
  }>;
  healthChecks: Array<{
    endpoint: string;
    status: 'pending' | 'passed' | 'failed';
    message?: string;
  }>;
  isCuttingOver: boolean;

  // Error handling
  error: string | null;

  // Actions
  setEntryPoint: (entryPoint: WizardEntryPoint) => void;
  goToStep: (step: WizardStep) => void;
  nextStep: () => void;
  previousStep: () => void;
  markStepComplete: (step: WizardStep) => void;

  // Analysis actions
  setSourceType: (type: WizardState['sourceType']) => void;
  setSourcePath: (path: string) => void;
  setSourceProvider: (provider: WizardState['sourceProvider']) => void;
  setAnalysisResult: (result: AnalyzeResponse | null) => void;
  setSelectedResources: (resources: Resource[]) => void;
  setIsAnalyzing: (analyzing: boolean) => void;

  // Cloud credentials actions
  setAwsCredentials: (creds: Partial<AWSCredentials>) => void;
  setGcpCredentials: (creds: Partial<GCPCredentials>) => void;
  setAzureCredentials: (creds: Partial<AzureCredentials>) => void;

  // Discovery progress actions
  setDiscoveryProgress: (progress: DiscoverProgressEvent | null) => void;
  setIsDiscovering: (discovering: boolean) => void;
  setDiscoveryError: (error: string | null) => void;

  // Saved discovery actions
  setSavedDiscoveryId: (id: string | null) => void;

  // Review phase actions
  setSelectedResourceForDetail: (resource: Resource | null) => void;
  setConsolidationPreview: (preview: ConsolidationPreview | null) => void;

  // Export actions
  setBundleId: (id: string | null) => void;
  setBundleName: (name: string) => void;
  setBundleManifest: (manifest: BundleManifest | null) => void;
  setDomain: (domain: string) => void;
  setConsolidate: (consolidate: boolean) => void;
  setIsExporting: (exporting: boolean) => void;
  setUploadedBundle: (file: File | null) => void;

  // Secrets actions
  setSecretRefs: (refs: SecretReference[]) => void;
  setSecretValue: (name: string, value: string) => void;
  setSecretsResolved: (resolved: boolean) => void;

  // Deploy actions
  setDeployTarget: (target: DeployTarget | null) => void;
  updateSSHConfig: (config: Partial<SSHConfig>) => void;
  setDeploymentId: (id: string | null) => void;
  setDeployProgress: (progress: number) => void;
  setIsDeploying: (deploying: boolean) => void;

  // Sync actions
  setSyncPlanId: (id: string | null) => void;
  setSyncTasks: (tasks: SyncTask[]) => void;
  updateSyncTask: (taskId: string, updates: Partial<SyncTask>) => void;
  setSyncProgress: (progress: number) => void;
  setIsSyncing: (syncing: boolean) => void;

  // Cutover actions
  setCutoverPlanId: (id: string | null) => void;
  setDnsChanges: (changes: WizardState['dnsChanges']) => void;
  setHealthChecks: (checks: WizardState['healthChecks']) => void;
  updateHealthCheck: (endpoint: string, status: 'pending' | 'passed' | 'failed', message?: string) => void;
  setIsCuttingOver: (cuttingOver: boolean) => void;

  // Error handling
  setError: (error: string | null) => void;
  clearError: () => void;

  // Reset
  reset: () => void;
}

const defaultSSHConfig: SSHConfig = {
  host: '',
  port: 22,
  username: '',
  authMethod: 'key',
  keyPath: '~/.ssh/id_rsa',
  password: '',
};

const defaultAWSCredentials: AWSCredentials = {
  accessKeyId: '',
  secretAccessKey: '',
};

const defaultGCPCredentials: GCPCredentials = {
  projectId: '',
  serviceAccountJson: '',
};

const defaultAzureCredentials: AzureCredentials = {
  subscriptionId: '',
  tenantId: '',
  clientId: '',
  clientSecret: '',
};

const initialState = {
  entryPoint: null as WizardEntryPoint,
  currentStep: 'analyze' as WizardStep,
  completedSteps: [] as WizardStep[],

  sourceType: null,
  sourcePath: '',
  sourceProvider: null,
  analysisResult: null,
  selectedResources: [] as Resource[],
  isAnalyzing: false,

  awsCredentials: { ...defaultAWSCredentials },
  gcpCredentials: { ...defaultGCPCredentials },
  azureCredentials: { ...defaultAzureCredentials },

  discoveryProgress: null,
  isDiscovering: false,
  discoveryError: null,

  savedDiscoveryId: null,

  selectedResourceForDetail: null,
  consolidationPreview: null,

  bundleId: null,
  bundleName: '',
  bundleManifest: null,
  domain: '',
  consolidate: true,
  isExporting: false,
  uploadedBundle: null,

  secretRefs: [] as SecretReference[],
  secretValues: {} as Record<string, string>,
  secretsResolved: false,

  deployTarget: null,
  sshConfig: { ...defaultSSHConfig },
  deploymentId: null,
  deployProgress: 0,
  isDeploying: false,

  syncPlanId: null,
  syncTasks: [] as SyncTask[],
  syncProgress: 0,
  isSyncing: false,

  cutoverPlanId: null,
  dnsChanges: [] as WizardState['dnsChanges'],
  healthChecks: [] as WizardState['healthChecks'],
  isCuttingOver: false,

  error: null,
};

export const useWizardStore = create<WizardState>((set, get) => ({
  ...initialState,

  // Navigation
  setEntryPoint: (entryPoint) => {
    const step = entryPoint === 'bundle' ? 'upload' : 'analyze';
    set({ entryPoint, currentStep: step });
  },

  goToStep: (step) => set({ currentStep: step }),

  nextStep: () => {
    const { currentStep, completedSteps, entryPoint } = get();
    const steps = entryPoint === 'bundle' ? BUNDLE_WIZARD_STEPS : SOURCE_WIZARD_STEPS;
    const currentIndex = steps.indexOf(currentStep);

    if (currentIndex < steps.length - 1) {
      const nextStep = steps[currentIndex + 1];
      set({
        currentStep: nextStep,
        completedSteps: completedSteps.includes(currentStep)
          ? completedSteps
          : [...completedSteps, currentStep],
      });
    }
  },

  previousStep: () => {
    const { currentStep, entryPoint } = get();
    const steps = entryPoint === 'bundle' ? BUNDLE_WIZARD_STEPS : SOURCE_WIZARD_STEPS;
    const currentIndex = steps.indexOf(currentStep);

    if (currentIndex > 0) {
      set({ currentStep: steps[currentIndex - 1] });
    }
  },

  markStepComplete: (step) => {
    const { completedSteps } = get();
    if (!completedSteps.includes(step)) {
      set({ completedSteps: [...completedSteps, step] });
    }
  },

  // Analysis actions
  setSourceType: (sourceType) => set({ sourceType }),
  setSourcePath: (sourcePath) => set({ sourcePath }),
  setSourceProvider: (sourceProvider) => set({ sourceProvider }),
  setAnalysisResult: (analysisResult) => {
    set({
      analysisResult,
      selectedResources: analysisResult?.resources || [],
    });
  },
  setSelectedResources: (selectedResources) => set({ selectedResources }),
  setIsAnalyzing: (isAnalyzing) => set({ isAnalyzing }),

  // Cloud credentials actions
  setAwsCredentials: (creds) => {
    const { awsCredentials } = get();
    set({ awsCredentials: { ...awsCredentials, ...creds } });
  },
  setGcpCredentials: (creds) => {
    const { gcpCredentials } = get();
    set({ gcpCredentials: { ...gcpCredentials, ...creds } });
  },
  setAzureCredentials: (creds) => {
    const { azureCredentials } = get();
    set({ azureCredentials: { ...azureCredentials, ...creds } });
  },

  // Discovery progress actions
  setDiscoveryProgress: (discoveryProgress) => set({ discoveryProgress }),
  setIsDiscovering: (isDiscovering) => set({ isDiscovering }),
  setDiscoveryError: (discoveryError) => set({ discoveryError }),

  // Saved discovery actions
  setSavedDiscoveryId: (savedDiscoveryId) => set({ savedDiscoveryId }),

  // Review phase actions
  setSelectedResourceForDetail: (selectedResourceForDetail) => set({ selectedResourceForDetail }),
  setConsolidationPreview: (consolidationPreview) => set({ consolidationPreview }),

  // Export actions
  setBundleId: (bundleId) => set({ bundleId }),
  setBundleName: (bundleName) => set({ bundleName }),
  setBundleManifest: (bundleManifest) => set({ bundleManifest }),
  setDomain: (domain) => set({ domain }),
  setConsolidate: (consolidate) => set({ consolidate }),
  setIsExporting: (isExporting) => set({ isExporting }),
  setUploadedBundle: (uploadedBundle) => set({ uploadedBundle }),

  // Secrets actions
  setSecretRefs: (secretRefs) => set({ secretRefs }),
  setSecretValue: (name, value) => {
    const { secretValues } = get();
    set({ secretValues: { ...secretValues, [name]: value } });
  },
  setSecretsResolved: (secretsResolved) => set({ secretsResolved }),

  // Deploy actions
  setDeployTarget: (deployTarget) => set({ deployTarget }),
  updateSSHConfig: (config) => {
    const { sshConfig } = get();
    set({ sshConfig: { ...sshConfig, ...config } });
  },
  setDeploymentId: (deploymentId) => set({ deploymentId }),
  setDeployProgress: (deployProgress) => set({ deployProgress }),
  setIsDeploying: (isDeploying) => set({ isDeploying }),

  // Sync actions
  setSyncPlanId: (syncPlanId) => set({ syncPlanId }),
  setSyncTasks: (syncTasks) => set({ syncTasks }),
  updateSyncTask: (taskId, updates) => {
    const { syncTasks } = get();
    set({
      syncTasks: syncTasks.map(task =>
        task.id === taskId ? { ...task, ...updates } : task
      ),
    });
  },
  setSyncProgress: (syncProgress) => set({ syncProgress }),
  setIsSyncing: (isSyncing) => set({ isSyncing }),

  // Cutover actions
  setCutoverPlanId: (cutoverPlanId) => set({ cutoverPlanId }),
  setDnsChanges: (dnsChanges) => set({ dnsChanges }),
  setHealthChecks: (healthChecks) => set({ healthChecks }),
  updateHealthCheck: (endpoint, status, message) => {
    const { healthChecks } = get();
    set({
      healthChecks: healthChecks.map(check =>
        check.endpoint === endpoint ? { ...check, status, message } : check
      ),
    });
  },
  setIsCuttingOver: (isCuttingOver) => set({ isCuttingOver }),

  // Error handling
  setError: (error) => set({ error }),
  clearError: () => set({ error: null }),

  // Reset
  reset: () => set(initialState),
}));
