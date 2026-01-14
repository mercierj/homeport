import { useState, useMemo } from 'react';
import { Code, Server, Layers, Box, Plus, Trash2, Clock, AlertTriangle, Info } from 'lucide-react';
import { ServiceMigrationCard } from '../ServiceMigrationCard';
import { useMigrationConfigStore } from '@/stores/migration-config';
import type { FunctionMigration } from '../types';
import type { Resource } from '@/lib/migrate-api';

// Resource type matchers for filtering which cards to show
const LAMBDA_TYPES = ['aws_lambda_function', 'google_cloudfunctions_function', 'google_cloudfunctions2_function', 'azurerm_function_app'];
const EC2_TYPES = ['aws_instance', 'aws_launch_template', 'google_compute_instance', 'azurerm_virtual_machine'];
const ECS_TYPES = ['aws_ecs_cluster', 'aws_ecs_service', 'aws_ecs_task_definition'];
const EKS_TYPES = ['aws_eks_cluster', 'google_container_cluster', 'azurerm_kubernetes_cluster'];

// ============================================================================
// Helper Components
// ============================================================================

interface InfoBoxProps {
  children: React.ReactNode;
}

function InfoBox({ children }: InfoBoxProps) {
  return (
    <div className="flex items-start gap-2 p-3 bg-info/10 border border-info/50 rounded-md">
      <Info className="w-4 h-4 text-info flex-shrink-0 mt-0.5" />
      <span className="text-sm text-info">{children}</span>
    </div>
  );
}

interface WarningBoxProps {
  children: React.ReactNode;
}

function WarningBox({ children }: WarningBoxProps) {
  return (
    <div className="flex items-start gap-2 p-3 bg-warning/10 border border-warning/50 rounded-md">
      <AlertTriangle className="w-4 h-4 text-warning flex-shrink-0 mt-0.5" />
      <span className="text-sm text-warning">{children}</span>
    </div>
  );
}

// ============================================================================
// Lambda Function List Component
// ============================================================================

interface FunctionListProps {
  functions: FunctionMigration[];
  onFunctionsChange: (functions: FunctionMigration[]) => void;
}

function FunctionList({ functions, onFunctionsChange }: FunctionListProps) {
  const [newFunction, setNewFunction] = useState<FunctionMigration>({
    functionArn: '',
    functionName: '',
    targetContainerName: '',
    runtime: 'nodejs18.x',
  });

  const handleAddFunction = () => {
    if (newFunction.functionArn.trim() && newFunction.functionName.trim() && newFunction.targetContainerName.trim()) {
      onFunctionsChange([...functions, { ...newFunction }]);
      setNewFunction({
        functionArn: '',
        functionName: '',
        targetContainerName: '',
        runtime: 'nodejs18.x',
      });
    }
  };

  const handleRemoveFunction = (index: number) => {
    onFunctionsChange(functions.filter((_, i) => i !== index));
  };

  const handleUpdateFunction = (index: number, field: keyof FunctionMigration, value: string) => {
    const updated = functions.map((fn, i) =>
      i === index ? { ...fn, [field]: value } : fn
    );
    onFunctionsChange(updated);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      handleAddFunction();
    }
  };

  const runtimeOptions = [
    'nodejs18.x',
    'nodejs20.x',
    'python3.9',
    'python3.10',
    'python3.11',
    'python3.12',
    'java17',
    'java21',
    'go1.x',
    'dotnet6',
    'dotnet8',
    'ruby3.2',
    'ruby3.3',
  ];

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <label className="block text-sm font-medium text-gray-700">
          Lambda Functions
        </label>
        <span className="text-xs text-gray-500">
          {functions.length} function{functions.length !== 1 ? 's' : ''} configured
        </span>
      </div>

      {/* Function Table */}
      <div className="border border-gray-200 rounded-lg overflow-hidden">
        {/* Table Header */}
        <div className="bg-muted border-b border-border px-4 py-2">
          <div className="grid grid-cols-[1fr_1fr_1fr_120px_auto] gap-3 text-xs font-medium text-gray-500 uppercase tracking-wider">
            <span>Function ARN</span>
            <span>Function Name</span>
            <span>Target Container</span>
            <span>Runtime</span>
            <span className="w-10"></span>
          </div>
        </div>

        {/* Existing Functions */}
        {functions.length > 0 && (
          <div className="divide-y divide-gray-100">
            {functions.map((fn, index) => (
              <div key={index} className="px-4 py-2 bg-white hover:bg-muted transition-colors">
                <div className="grid grid-cols-[1fr_1fr_1fr_120px_auto] gap-3 items-center">
                  <input
                    type="text"
                    value={fn.functionArn}
                    onChange={(e) => handleUpdateFunction(index, 'functionArn', e.target.value)}
                    placeholder="arn:aws:lambda:..."
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <input
                    type="text"
                    value={fn.functionName}
                    onChange={(e) => handleUpdateFunction(index, 'functionName', e.target.value)}
                    placeholder="my-function"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <input
                    type="text"
                    value={fn.targetContainerName}
                    onChange={(e) => handleUpdateFunction(index, 'targetContainerName', e.target.value)}
                    placeholder="my-function-container"
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  />
                  <select
                    value={fn.runtime}
                    onChange={(e) => handleUpdateFunction(index, 'runtime', e.target.value)}
                    className="w-full px-2 py-1.5 text-sm border border-gray-200 rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
                  >
                    {runtimeOptions.map((runtime) => (
                      <option key={runtime} value={runtime}>
                        {runtime}
                      </option>
                    ))}
                  </select>
                  <button
                    type="button"
                    onClick={() => handleRemoveFunction(index)}
                    className="p-1.5 text-muted-foreground/60 hover:text-error hover:bg-error/10 rounded transition-colors"
                    aria-label="Remove function"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Add New Function Row */}
        <div className="px-4 py-2 bg-muted border-t border-border">
          <div className="grid grid-cols-[1fr_1fr_1fr_120px_auto] gap-3 items-center">
            <input
              type="text"
              value={newFunction.functionArn}
              onChange={(e) => setNewFunction({ ...newFunction, functionArn: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="arn:aws:lambda:..."
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <input
              type="text"
              value={newFunction.functionName}
              onChange={(e) => setNewFunction({ ...newFunction, functionName: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="Function name"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <input
              type="text"
              value={newFunction.targetContainerName}
              onChange={(e) => setNewFunction({ ...newFunction, targetContainerName: e.target.value })}
              onKeyDown={handleKeyDown}
              placeholder="Target container"
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            />
            <select
              value={newFunction.runtime}
              onChange={(e) => setNewFunction({ ...newFunction, runtime: e.target.value })}
              className="w-full px-2 py-1.5 text-sm border border-input rounded focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
            >
              {runtimeOptions.map((runtime) => (
                <option key={runtime} value={runtime}>
                  {runtime}
                </option>
              ))}
            </select>
            <button
              type="button"
              onClick={handleAddFunction}
              disabled={!newFunction.functionArn.trim() || !newFunction.functionName.trim() || !newFunction.targetContainerName.trim()}
              className="p-1.5 text-primary hover:bg-primary/10 rounded transition-colors disabled:text-muted-foreground/40 disabled:hover:bg-transparent disabled:cursor-not-allowed"
              aria-label="Add function"
            >
              <Plus className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Empty State */}
        {functions.length === 0 && (
          <div className="px-4 py-6 text-center text-sm text-gray-500">
            No functions configured. Add a Lambda function above.
          </div>
        )}
      </div>
    </div>
  );
}

// ============================================================================
// EC2 Configuration Types and Component
// ============================================================================

interface EC2Config {
  enabled: boolean;
  instances: string[];
  extractUserDataScripts: boolean;
  convertSecurityGroups: boolean;
  generateDockerfile: boolean;
}

interface EC2MigrationSectionProps {
  config: EC2Config;
  onConfigChange: (config: Partial<EC2Config>) => void;
}

function EC2MigrationSection({ config, onConfigChange }: EC2MigrationSectionProps) {
  const [instanceInput, setInstanceInput] = useState(config.instances.join(', '));

  const handleInstancesChange = (value: string) => {
    setInstanceInput(value);
    const instances = value
      .split(',')
      .map((i) => i.trim())
      .filter((i) => i.length > 0);
    onConfigChange({ instances });
  };

  return (
    <div className="space-y-4">
      {/* Instance Selection */}
      <div className="space-y-1">
        <label htmlFor="ec2-instances" className="block text-sm font-medium text-gray-700">
          EC2 Instance IDs
        </label>
        <input
          id="ec2-instances"
          type="text"
          value={instanceInput}
          onChange={(e) => handleInstancesChange(e.target.value)}
          placeholder="i-0123456789abcdef0, i-0987654321fedcba0"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500">
          Enter instance IDs separated by commas
        </p>
      </div>

      {/* Options */}
      <div className="space-y-3">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.extractUserDataScripts}
            onChange={(e) => onConfigChange({ extractUserDataScripts: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Extract User Data Scripts</span>
            <p className="text-xs text-gray-500">
              Convert EC2 user data scripts to container entrypoints
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.convertSecurityGroups}
            onChange={(e) => onConfigChange({ convertSecurityGroups: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Convert Security Groups</span>
            <p className="text-xs text-gray-500">
              Transform security group rules to Docker firewall rules
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.generateDockerfile}
            onChange={(e) => onConfigChange({ generateDockerfile: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Generate Dockerfile</span>
            <p className="text-xs text-gray-500">
              Create Dockerfiles based on AMI and instance configuration
            </p>
          </div>
        </label>
      </div>

      <WarningBox>
        Stateful instances may require manual migration. Review generated configurations carefully
        and test thoroughly before production deployment.
      </WarningBox>
    </div>
  );
}

// ============================================================================
// ECS Configuration Types and Component
// ============================================================================

interface ECSConfig {
  enabled: boolean;
  taskDefinitions: string[];
  includeSecrets: boolean;
  includeEnvironmentVariables: boolean;
  convertServiceDiscovery: boolean;
}

interface ECSMigrationSectionProps {
  config: ECSConfig;
  onConfigChange: (config: Partial<ECSConfig>) => void;
}

function ECSMigrationSection({ config, onConfigChange }: ECSMigrationSectionProps) {
  const [taskDefInput, setTaskDefInput] = useState(config.taskDefinitions.join(', '));

  const handleTaskDefinitionsChange = (value: string) => {
    setTaskDefInput(value);
    const taskDefinitions = value
      .split(',')
      .map((t) => t.trim())
      .filter((t) => t.length > 0);
    onConfigChange({ taskDefinitions });
  };

  return (
    <div className="space-y-4">
      {/* Task Definition Selection */}
      <div className="space-y-1">
        <label htmlFor="ecs-task-definitions" className="block text-sm font-medium text-gray-700">
          Task Definition ARNs
        </label>
        <input
          id="ecs-task-definitions"
          type="text"
          value={taskDefInput}
          onChange={(e) => handleTaskDefinitionsChange(e.target.value)}
          placeholder="arn:aws:ecs:region:account:task-definition/name:revision"
          className="w-full px-3 py-2 border border-input rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-blue-500"
        />
        <p className="text-xs text-gray-500">
          Enter task definition ARNs separated by commas
        </p>
      </div>

      {/* Options */}
      <div className="space-y-3">
        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeSecrets}
            onChange={(e) => onConfigChange({ includeSecrets: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include Secrets</span>
            <p className="text-xs text-gray-500">
              Migrate secrets referenced in task definitions
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.includeEnvironmentVariables}
            onChange={(e) => onConfigChange({ includeEnvironmentVariables: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Include Environment Variables</span>
            <p className="text-xs text-gray-500">
              Export environment variables to Docker Compose
            </p>
          </div>
        </label>

        <label className="flex items-center gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={config.convertServiceDiscovery}
            onChange={(e) => onConfigChange({ convertServiceDiscovery: e.target.checked })}
            className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
          />
          <div>
            <span className="text-sm font-medium text-gray-700">Convert Service Discovery</span>
            <p className="text-xs text-gray-500">
              Map ECS service discovery to Docker network aliases
            </p>
          </div>
        </label>
      </div>

      <InfoBox>
        ECS task definitions will be converted to Docker Compose service definitions.
        Container images, port mappings, and resource limits will be preserved.
      </InfoBox>
    </div>
  );
}

// ============================================================================
// EKS Configuration Types and Component (Coming Soon)
// ============================================================================

interface EKSConfig {
  enabled: boolean;
  targetPlatform: 'k3s' | 'docker-swarm' | 'docker-compose';
}

interface EKSMigrationSectionProps {
  config: EKSConfig;
  onConfigChange: (config: Partial<EKSConfig>) => void;
}

function EKSMigrationSection({ config, onConfigChange }: EKSMigrationSectionProps) {
  return (
    <div className="space-y-4">
      <div className="flex flex-col items-center justify-center py-6 text-center">
        <div className="w-12 h-12 rounded-full bg-muted flex items-center justify-center mb-3">
          <Clock className="w-6 h-6 text-muted-foreground/60" />
        </div>
        <p className="text-sm font-medium text-gray-600">Coming Soon</p>
        <p className="text-xs text-muted-foreground/60 mt-1 mb-4">
          EKS migration support is under development
        </p>
      </div>

      {/* Target Platform Selection (for future use) */}
      <div className="space-y-2">
        <label className="block text-sm font-medium text-gray-700">
          Target Platform (Preview)
        </label>
        <div className="flex gap-3">
          {[
            { value: 'k3s', label: 'K3s' },
            { value: 'docker-swarm', label: 'Docker Swarm' },
            { value: 'docker-compose', label: 'Docker Compose' },
          ].map((option) => (
            <button
              key={option.value}
              type="button"
              onClick={() => onConfigChange({ targetPlatform: option.value as EKSConfig['targetPlatform'] })}
              className={`px-4 py-2 text-sm rounded-md border transition-colors ${
                config.targetPlatform === option.value
                  ? 'bg-primary/10 border-primary text-primary'
                  : 'bg-white border-input text-gray-700 hover:bg-muted'
              }`}
            >
              {option.label}
            </button>
          ))}
        </div>
        <p className="text-xs text-gray-500">
          Select the target platform for Kubernetes workload migration
        </p>
      </div>
    </div>
  );
}

// ============================================================================
// Main ComputeMigration Component
// ============================================================================

interface ComputeMigrationProps {
  resources: Resource[];
}

export function ComputeMigration({ resources = [] }: ComputeMigrationProps) {
  const { functions, setFunctionsConfig } = useMigrationConfigStore();

  // Determine which service cards to show based on discovered resources
  const { hasLambda, hasEC2, hasECS, hasEKS } = useMemo(() => ({
    hasLambda: resources.some(r => LAMBDA_TYPES.includes(r.type)),
    hasEC2: resources.some(r => EC2_TYPES.includes(r.type)),
    hasECS: resources.some(r => ECS_TYPES.includes(r.type)),
    hasEKS: resources.some(r => EKS_TYPES.includes(r.type)),
  }), [resources]);

  // Local state for EC2, ECS, EKS (not in current types)
  const [ec2Config, setEC2Config] = useState<EC2Config>({
    enabled: false,
    instances: [],
    extractUserDataScripts: true,
    convertSecurityGroups: true,
    generateDockerfile: true,
  });

  const [ecsConfig, setECSConfig] = useState<ECSConfig>({
    enabled: false,
    taskDefinitions: [],
    includeSecrets: true,
    includeEnvironmentVariables: true,
    convertServiceDiscovery: true,
  });

  const [eksConfig, setEKSConfig] = useState<EKSConfig>({
    enabled: false,
    targetPlatform: 'k3s',
  });

  const handleEC2Change = (config: Partial<EC2Config>) => {
    setEC2Config((prev) => ({ ...prev, ...config }));
  };

  const handleECSChange = (config: Partial<ECSConfig>) => {
    setECSConfig((prev) => ({ ...prev, ...config }));
  };

  const handleEKSChange = (config: Partial<EKSConfig>) => {
    setEKSConfig((prev) => ({ ...prev, ...config }));
  };

  const handleFunctionsChange = (functionsList: FunctionMigration[]) => {
    setFunctionsConfig({ functions: functionsList });
  };

  // If no compute resources discovered, show empty state
  if (!hasLambda && !hasEC2 && !hasECS && !hasEKS) {
    return (
      <div className="text-center py-8 text-muted-foreground">
        <Code className="w-12 h-12 mx-auto mb-3 opacity-50" />
        <p className="font-medium">No compute resources discovered</p>
        <p className="text-sm mt-1">Lambda, EC2, ECS, or EKS resources will appear here when detected.</p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Lambda Section - only show if Lambda resources discovered */}
      {hasLambda && (
        <ServiceMigrationCard
          title="AWS Lambda → Docker Containers"
          description="Package Lambda functions as Docker containers"
          icon={Code}
          enabled={functions.enabled}
          onToggle={(enabled) => setFunctionsConfig({ enabled })}
          defaultExpanded={true}
        >
          <div className="space-y-6">
            {/* Function List */}
            <FunctionList
              functions={functions.functions}
              onFunctionsChange={handleFunctionsChange}
            />

            {/* Options */}
            <div className="space-y-3">
              <label className="flex items-center gap-3 cursor-pointer">
                <input
                  type="checkbox"
                  checked={functions.includeEnvironmentVariables}
                  onChange={(e) => setFunctionsConfig({ includeEnvironmentVariables: e.target.checked })}
                  className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
                />
                <div>
                  <span className="text-sm font-medium text-gray-700">Include Environment Variables</span>
                  <p className="text-xs text-gray-500">
                    Export Lambda environment variables to container configuration
                  </p>
                </div>
              </label>

              <label className="flex items-center gap-3 cursor-pointer">
                <input
                  type="checkbox"
                  checked={functions.includeLayers}
                  onChange={(e) => setFunctionsConfig({ includeLayers: e.target.checked })}
                  className="w-4 h-4 text-primary rounded border-input focus:ring-blue-500"
                />
                <div>
                  <span className="text-sm font-medium text-gray-700">Include Layers</span>
                  <p className="text-xs text-gray-500">
                    Package Lambda layers into the container image
                  </p>
                </div>
              </label>
            </div>

            <InfoBox>
              Lambda functions will be packaged using AWS Lambda Runtime Interface Client (RIC).
              Ensure your functions are compatible with container-based execution.
            </InfoBox>
          </div>
        </ServiceMigrationCard>
      )}

      {/* EC2 Section - only show if EC2 resources discovered */}
      {hasEC2 && (
        <ServiceMigrationCard
          title="Amazon EC2 → Docker"
          description="Convert EC2 configurations to Docker Compose"
          icon={Server}
          enabled={ec2Config.enabled}
          onToggle={(enabled) => handleEC2Change({ enabled })}
          defaultExpanded={true}
        >
          <EC2MigrationSection
            config={ec2Config}
            onConfigChange={handleEC2Change}
          />
        </ServiceMigrationCard>
      )}

      {/* ECS Section - only show if ECS resources discovered */}
      {hasECS && (
        <ServiceMigrationCard
          title="Amazon ECS → Docker Compose"
          description="Convert ECS task definitions to Docker Compose"
          icon={Layers}
          enabled={ecsConfig.enabled}
          onToggle={(enabled) => handleECSChange({ enabled })}
          defaultExpanded={true}
        >
          <ECSMigrationSection
            config={ecsConfig}
            onConfigChange={handleECSChange}
          />
        </ServiceMigrationCard>
      )}

      {/* EKS Section - only show if EKS resources discovered */}
      {hasEKS && (
        <ServiceMigrationCard
          title="Amazon EKS → K3s/Docker Swarm"
          description="Migrate Kubernetes workloads"
          icon={Box}
          enabled={eksConfig.enabled}
          onToggle={(enabled) => handleEKSChange({ enabled })}
          defaultExpanded={true}
        >
          <EKSMigrationSection
            config={eksConfig}
            onConfigChange={handleEKSChange}
          />
        </ServiceMigrationCard>
      )}
    </div>
  );
}

export default ComputeMigration;
