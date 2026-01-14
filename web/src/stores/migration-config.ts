import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';
import type {
  MigrationConfiguration,
  MigrationCategory,
  MigrationProgress,
  MigrationStatus,
  MigrationOptions,
  SourceCredentials,
  DatabaseConfig,
  StorageConfig,
  QueueConfig,
  CacheConfig,
  AuthConfig,
  SecretsConfig,
  DNSConfig,
  FunctionsConfig,
  ValidationResult,
  MigrationLogEvent,
} from '@/components/MigrationWizard/types';
import {
  DEFAULT_DATABASE_CONFIG,
  DEFAULT_STORAGE_CONFIG,
  DEFAULT_QUEUE_CONFIG,
  DEFAULT_CACHE_CONFIG,
  DEFAULT_AUTH_CONFIG,
  DEFAULT_SECRETS_CONFIG,
  DEFAULT_DNS_CONFIG,
  DEFAULT_FUNCTIONS_CONFIG,
  DEFAULT_MIGRATION_OPTIONS,
  MIGRATION_CATEGORIES,
} from '@/components/MigrationWizard/types';

// ============================================================================
// Store State Interface
// ============================================================================

export interface MigrationConfigState {
  // Current step in the wizard
  currentStep: 'configure' | 'validate' | 'execute' | 'complete';

  // Active category being configured
  activeCategory: MigrationCategory;

  // Source credentials
  credentials: SourceCredentials;

  // Per-category configurations
  database: DatabaseConfig;
  storage: StorageConfig;
  queue: QueueConfig;
  cache: CacheConfig;
  auth: AuthConfig;
  secrets: SecretsConfig;
  dns: DNSConfig;
  functions: FunctionsConfig;

  // Global options
  options: MigrationOptions;

  // Validation state
  validationResults: ValidationResult[];
  isValidating: boolean;
  validationError: string | null;

  // Execution state
  migrationId: string | null;
  migrationStatus: MigrationStatus | null;
  migrationProgress: MigrationProgress | null;
  migrationLogs: MigrationLogEvent[];
  isExecuting: boolean;
  executionError: string | null;

  // Actions - Navigation
  setCurrentStep: (step: MigrationConfigState['currentStep']) => void;
  setActiveCategory: (category: MigrationCategory) => void;

  // Actions - Credentials
  setCredentials: (credentials: Partial<SourceCredentials>) => void;

  // Actions - Category configurations
  setDatabaseConfig: (config: Partial<DatabaseConfig>) => void;
  setStorageConfig: (config: Partial<StorageConfig>) => void;
  setQueueConfig: (config: Partial<QueueConfig>) => void;
  setCacheConfig: (config: Partial<CacheConfig>) => void;
  setAuthConfig: (config: Partial<AuthConfig>) => void;
  setSecretsConfig: (config: Partial<SecretsConfig>) => void;
  setDNSConfig: (config: Partial<DNSConfig>) => void;
  setFunctionsConfig: (config: Partial<FunctionsConfig>) => void;

  // Actions - Options
  setOptions: (options: Partial<MigrationOptions>) => void;

  // Actions - Validation
  setValidationResults: (results: ValidationResult[]) => void;
  setIsValidating: (isValidating: boolean) => void;
  setValidationError: (error: string | null) => void;

  // Actions - Execution
  setMigrationId: (id: string | null) => void;
  setMigrationStatus: (status: MigrationStatus | null) => void;
  setMigrationProgress: (progress: MigrationProgress | null) => void;
  addMigrationLog: (log: MigrationLogEvent) => void;
  clearMigrationLogs: () => void;
  setIsExecuting: (isExecuting: boolean) => void;
  setExecutionError: (error: string | null) => void;

  // Actions - Helpers
  getConfiguration: () => MigrationConfiguration;
  getEnabledCategories: () => MigrationCategory[];
  getCategoryConfig: (category: MigrationCategory) => DatabaseConfig | StorageConfig | QueueConfig | CacheConfig | AuthConfig | SecretsConfig | DNSConfig | FunctionsConfig;
  setCategoryEnabled: (category: MigrationCategory, enabled: boolean) => void;
  isConfigurationValid: () => boolean;

  // Actions - Reset
  reset: () => void;
  resetValidation: () => void;
  resetExecution: () => void;
}

// ============================================================================
// Default Credentials
// ============================================================================

const DEFAULT_CREDENTIALS: SourceCredentials = {
  provider: 'aws',
};

// ============================================================================
// Store Implementation
// ============================================================================

export const useMigrationConfigStore = create<MigrationConfigState>()(
  persist(
    (set, get) => ({
      // Initial state
      currentStep: 'configure',
      activeCategory: 'database',

      credentials: { ...DEFAULT_CREDENTIALS },

      database: { ...DEFAULT_DATABASE_CONFIG },
      storage: { ...DEFAULT_STORAGE_CONFIG },
      queue: { ...DEFAULT_QUEUE_CONFIG },
      cache: { ...DEFAULT_CACHE_CONFIG },
      auth: { ...DEFAULT_AUTH_CONFIG },
      secrets: { ...DEFAULT_SECRETS_CONFIG },
      dns: { ...DEFAULT_DNS_CONFIG },
      functions: { ...DEFAULT_FUNCTIONS_CONFIG },

      options: { ...DEFAULT_MIGRATION_OPTIONS },

      validationResults: [],
      isValidating: false,
      validationError: null,

      migrationId: null,
      migrationStatus: null,
      migrationProgress: null,
      migrationLogs: [],
      isExecuting: false,
      executionError: null,

      // Navigation actions
      setCurrentStep: (step) => set({ currentStep: step }),
      setActiveCategory: (category) => set({ activeCategory: category }),

      // Credentials action
      setCredentials: (credentials) =>
        set((state) => ({
          credentials: { ...state.credentials, ...credentials },
        })),

      // Category configuration actions
      setDatabaseConfig: (config) =>
        set((state) => ({
          database: { ...state.database, ...config },
        })),

      setStorageConfig: (config) =>
        set((state) => ({
          storage: { ...state.storage, ...config },
        })),

      setQueueConfig: (config) =>
        set((state) => ({
          queue: { ...state.queue, ...config },
        })),

      setCacheConfig: (config) =>
        set((state) => ({
          cache: { ...state.cache, ...config },
        })),

      setAuthConfig: (config) =>
        set((state) => ({
          auth: { ...state.auth, ...config },
        })),

      setSecretsConfig: (config) =>
        set((state) => ({
          secrets: { ...state.secrets, ...config },
        })),

      setDNSConfig: (config) =>
        set((state) => ({
          dns: { ...state.dns, ...config },
        })),

      setFunctionsConfig: (config) =>
        set((state) => ({
          functions: { ...state.functions, ...config },
        })),

      // Options action
      setOptions: (options) =>
        set((state) => ({
          options: { ...state.options, ...options },
        })),

      // Validation actions
      setValidationResults: (results) => set({ validationResults: results }),
      setIsValidating: (isValidating) => set({ isValidating }),
      setValidationError: (error) => set({ validationError: error }),

      // Execution actions
      setMigrationId: (id) => set({ migrationId: id }),
      setMigrationStatus: (status) => set({ migrationStatus: status }),
      setMigrationProgress: (progress) => set({ migrationProgress: progress }),
      addMigrationLog: (log) =>
        set((state) => ({
          migrationLogs: [...state.migrationLogs.slice(-999), log], // Keep last 1000 logs
        })),
      clearMigrationLogs: () => set({ migrationLogs: [] }),
      setIsExecuting: (isExecuting) => set({ isExecuting }),
      setExecutionError: (error) => set({ executionError: error }),

      // Helper actions
      getConfiguration: () => {
        const state = get();
        return {
          sourceCredentials: state.credentials,
          database: state.database,
          storage: state.storage,
          queue: state.queue,
          cache: state.cache,
          auth: state.auth,
          secrets: state.secrets,
          dns: state.dns,
          functions: state.functions,
          options: state.options,
        };
      },

      getEnabledCategories: () => {
        const state = get();
        return MIGRATION_CATEGORIES.filter((category) => {
          const config = state[category];
          return config.enabled;
        });
      },

      getCategoryConfig: (category) => {
        const state = get();
        return state[category];
      },

      setCategoryEnabled: (category, enabled) => {
        const state = get();
        const setter = {
          database: state.setDatabaseConfig,
          storage: state.setStorageConfig,
          queue: state.setQueueConfig,
          cache: state.setCacheConfig,
          auth: state.setAuthConfig,
          secrets: state.setSecretsConfig,
          dns: state.setDNSConfig,
          functions: state.setFunctionsConfig,
        }[category];
        setter({ enabled });
      },

      isConfigurationValid: () => {
        const state = get();
        const enabledCategories = state.getEnabledCategories();

        if (enabledCategories.length === 0) {
          return false;
        }

        // Check credentials based on provider
        const creds = state.credentials;
        if (creds.provider === 'aws') {
          if (!creds.awsAccessKeyId || !creds.awsSecretAccessKey) {
            return false;
          }
        } else if (creds.provider === 'gcp') {
          if (!creds.gcpProjectId || !creds.gcpServiceAccountJson) {
            return false;
          }
        } else if (creds.provider === 'azure') {
          if (!creds.azureSubscriptionId || !creds.azureTenantId ||
              !creds.azureClientId || !creds.azureClientSecret) {
            return false;
          }
        }

        return true;
      },

      // Reset actions
      reset: () =>
        set({
          currentStep: 'configure',
          activeCategory: 'database',
          credentials: { ...DEFAULT_CREDENTIALS },
          database: { ...DEFAULT_DATABASE_CONFIG },
          storage: { ...DEFAULT_STORAGE_CONFIG },
          queue: { ...DEFAULT_QUEUE_CONFIG },
          cache: { ...DEFAULT_CACHE_CONFIG },
          auth: { ...DEFAULT_AUTH_CONFIG },
          secrets: { ...DEFAULT_SECRETS_CONFIG },
          dns: { ...DEFAULT_DNS_CONFIG },
          functions: { ...DEFAULT_FUNCTIONS_CONFIG },
          options: { ...DEFAULT_MIGRATION_OPTIONS },
          validationResults: [],
          isValidating: false,
          validationError: null,
          migrationId: null,
          migrationStatus: null,
          migrationProgress: null,
          migrationLogs: [],
          isExecuting: false,
          executionError: null,
        }),

      resetValidation: () =>
        set({
          validationResults: [],
          isValidating: false,
          validationError: null,
        }),

      resetExecution: () =>
        set({
          migrationId: null,
          migrationStatus: null,
          migrationProgress: null,
          migrationLogs: [],
          isExecuting: false,
          executionError: null,
        }),
    }),
    {
      name: 'migration-config-storage',
      storage: createJSONStorage(() => sessionStorage),
      partialize: (state) => ({
        // Only persist configuration, not execution state
        currentStep: state.currentStep,
        activeCategory: state.activeCategory,
        credentials: state.credentials,
        database: state.database,
        storage: state.storage,
        queue: state.queue,
        cache: state.cache,
        auth: state.auth,
        secrets: state.secrets,
        dns: state.dns,
        functions: state.functions,
        options: state.options,
      }),
    }
  )
);
