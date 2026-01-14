import { create } from 'zustand';
import type {
  DeployTarget,
  DeployConfig,
  LocalDeployConfig,
  SSHDeployConfig,
  PhaseEvent,
  LogEvent,
  ServiceStatus,
} from '@/lib/deploy-api';
import type { Provider, Region } from '@/lib/providers-api';

// HA Level for cloud deployments
export type HALevel = 'none' | 'basic' | 'multi' | 'cluster';

// Cloud provider configuration
export interface CloudProviderConfig {
  provider: Provider | null;
  region: Region | null;
  instanceType: string | null;
  haLevel: HALevel;
  domain: string;
  enableSSL: boolean;
  enableMonitoring: boolean;
  enableBackups: boolean;
}

export interface DeploymentState {
  // Current deployment
  deploymentId: string | null;
  status: 'idle' | 'configuring' | 'deploying' | 'completed' | 'failed' | 'cancelled';

  // Target selection
  target: DeployTarget | null;

  // Configuration
  localConfig: LocalDeployConfig;
  sshConfig: SSHDeployConfig;
  cloudConfig: CloudProviderConfig;

  // Progress tracking
  currentPhase: PhaseEvent | null;
  progress: number;
  logs: LogEvent[];

  // Result
  services: ServiceStatus[];
  error: string | null;

  // Actions
  setTarget: (target: DeployTarget | null) => void;
  updateLocalConfig: (config: Partial<LocalDeployConfig>) => void;
  updateSSHConfig: (config: Partial<SSHDeployConfig>) => void;
  updateCloudConfig: (config: Partial<CloudProviderConfig>) => void;
  setCloudProvider: (provider: Provider) => void;
  setDeploymentId: (id: string) => void;
  setStatus: (status: DeploymentState['status']) => void;
  setPhase: (phase: PhaseEvent) => void;
  setProgress: (percent: number) => void;
  addLog: (log: LogEvent) => void;
  setServices: (services: ServiceStatus[]) => void;
  setError: (error: string | null) => void;
  reset: () => void;
  getConfig: () => DeployConfig | null;
}

const defaultLocalConfig: LocalDeployConfig = {
  projectName: 'homeport-stack',
  dataDirectory: '~/.homeport/data',
  networkMode: 'bridge',
  autoStart: true,
  enableMonitoring: false,
  composeContent: '',
  scripts: {},
  runtime: 'auto',
};

const defaultSSHConfig: SSHDeployConfig = {
  host: '',
  port: 22,
  username: '',
  authMethod: 'key',
  keyPath: '~/.ssh/id_rsa',
  password: '',
  remoteDir: '/opt/homeport',
  composeContent: '',
  scripts: {},
  projectName: 'homeport-stack',
  runtime: 'auto',
};

const defaultCloudConfig: CloudProviderConfig = {
  provider: null,
  region: null,
  instanceType: null,
  haLevel: 'none',
  domain: '',
  enableSSL: true,
  enableMonitoring: false,
  enableBackups: false,
};

export const useDeploymentStore = create<DeploymentState>((set, get) => ({
  deploymentId: null,
  status: 'idle',
  target: null,
  localConfig: { ...defaultLocalConfig },
  sshConfig: { ...defaultSSHConfig },
  cloudConfig: { ...defaultCloudConfig },
  currentPhase: null,
  progress: 0,
  logs: [],
  services: [],
  error: null,

  setTarget: (target) => set({ target, status: target ? 'configuring' : 'idle' }),

  updateLocalConfig: (config) =>
    set((state) => ({
      localConfig: { ...state.localConfig, ...config },
    })),

  updateSSHConfig: (config) =>
    set((state) => ({
      sshConfig: { ...state.sshConfig, ...config },
    })),

  updateCloudConfig: (config) =>
    set((state) => ({
      cloudConfig: { ...state.cloudConfig, ...config },
    })),

  setCloudProvider: (provider) =>
    set((state) => ({
      cloudConfig: {
        ...state.cloudConfig,
        provider,
        region: null,
        instanceType: null,
      },
    })),

  setDeploymentId: (id) => set({ deploymentId: id }),

  setStatus: (status) => set({ status }),

  setPhase: (phase) => set({ currentPhase: phase }),

  setProgress: (percent) => set({ progress: percent }),

  addLog: (log) =>
    set((state) => ({
      logs: [...state.logs, log],
    })),

  setServices: (services) => set({ services }),

  setError: (error) => set({ error, status: error ? 'failed' : get().status }),

  reset: () =>
    set({
      deploymentId: null,
      status: 'idle',
      target: null,
      localConfig: { ...defaultLocalConfig },
      sshConfig: { ...defaultSSHConfig },
      cloudConfig: { ...defaultCloudConfig },
      currentPhase: null,
      progress: 0,
      logs: [],
      services: [],
      error: null,
    }),

  getConfig: () => {
    const state = get();
    if (state.target === 'local') {
      return state.localConfig;
    } else if (state.target === 'ssh') {
      return state.sshConfig;
    }
    return null;
  },
}));
