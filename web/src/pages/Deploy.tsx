import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { TargetSelector } from '../components/DeploymentWizard/TargetSelector';
import { ConfigurationForm } from '../components/DeploymentWizard/ConfigurationForm';
import { DeploymentExecution } from '../components/DeploymentWizard/DeploymentExecution';
import { useDeploymentStore } from '../stores/deployment';
import { startDeployment } from '../lib/deploy-api';
import { createPendingStack } from '../lib/stacks-api';
import { toast } from 'sonner';

type Step = 'target' | 'config' | 'deploy';

export function Deploy() {
  const [step, setStep] = useState<Step>('target');
  const { target, reset, setDeploymentId, getConfig, localConfig, sshConfig } = useDeploymentStore();
  const navigate = useNavigate();

  const saveMutation = useMutation({
    mutationFn: async () => {
      // Get stack name from config
      const stackName = target === 'local'
        ? localConfig.projectName
        : target === 'ssh'
          ? sshConfig.projectName
          : 'pending-deployment';

      if (!stackName) {
        throw new Error('Project name is required');
      }

      // Create deployment config - for local/SSH, we use 'self-hosted' as provider
      const deploymentConfig = {
        provider: 'hetzner' as const, // Default to hetzner for pending saves
        region: 'fsn1', // Default region
        ha_level: 'none',
      };

      return createPendingStack({
        name: stackName,
        description: `Saved ${target} deployment configuration`,
        deployment_config: deploymentConfig,
      });
    },
    onSuccess: () => {
      toast.success('Configuration saved!', {
        description: 'You can resume this deployment from the Stacks page.',
        action: {
          label: 'View Stacks',
          onClick: () => navigate('/stacks'),
        },
      });
      reset();
      setStep('target');
    },
    onError: (error: Error) => {
      toast.error(`Failed to save: ${error.message}`);
    },
  });

  const deployMutation = useMutation({
    mutationFn: async () => {
      const config = getConfig();
      if (!config || !target) {
        throw new Error('No deployment configuration');
      }
      return startDeployment(target, config);
    },
    onSuccess: (data) => {
      if (data.deployment_id) {
        setDeploymentId(data.deployment_id);
      }
      setStep('deploy');
    },
    onError: (error: Error) => {
      toast.error(`Deployment failed: ${error.message}`);
    },
  });

  const handleTargetSelect = () => {
    setStep('config');
  };

  const handleDeploy = () => {
    deployMutation.mutate();
  };

  const handleSaveForLater = () => {
    saveMutation.mutate();
  };

  const handleReset = () => {
    reset();
    setStep('target');
  };

  const handleRetry = () => {
    deployMutation.mutate();
  };

  const handleComplete = () => {
    toast.success('Deployment completed successfully!');
    handleReset();
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Deploy</h1>
          <p className="text-muted-foreground">Deploy your self-hosted stack to local Docker or remote servers</p>
        </div>
        {step !== 'target' && (
          <button
            onClick={handleReset}
            className="text-sm text-muted-foreground hover:text-foreground"
          >
            Start Over
          </button>
        )}
      </div>

      {/* Progress Steps */}
      <div className="flex items-center gap-4 text-sm">
        <div className={step === 'target' ? 'text-primary font-medium' : 'text-muted-foreground'}>
          1. Select Target
        </div>
        <div className="h-px w-8 bg-border" />
        <div className={step === 'config' ? 'text-primary font-medium' : 'text-muted-foreground'}>
          2. Configure
        </div>
        <div className="h-px w-8 bg-border" />
        <div className={step === 'deploy' ? 'text-primary font-medium' : 'text-muted-foreground'}>
          3. Deploy
        </div>
      </div>

      {/* Step Content */}
      {step === 'target' && <TargetSelector onSelect={handleTargetSelect} />}
      {step === 'config' && target && (
        <ConfigurationForm
          onBack={() => setStep('target')}
          onDeploy={handleDeploy}
          onSaveForLater={handleSaveForLater}
          isDeploying={deployMutation.isPending}
          isSaving={saveMutation.isPending}
        />
      )}
      {step === 'deploy' && (
        <DeploymentExecution
          onRetry={handleRetry}
          onComplete={handleComplete}
        />
      )}
    </div>
  );
}
