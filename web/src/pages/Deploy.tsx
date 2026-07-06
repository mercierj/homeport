import { useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { useNavigate } from 'react-router-dom';
import { TargetSelector } from '../components/DeploymentWizard/TargetSelector';
import { ConfigurationForm } from '../components/DeploymentWizard/ConfigurationForm';
import { DeploymentExecution } from '../components/DeploymentWizard/DeploymentExecution';
import { ProviderComparison } from '../components/DeploymentWizard/ProviderComparison';
import { ProviderConfigForm } from '../components/DeploymentWizard/ProviderConfigForm';
import { TerraformExport } from '../components/DeploymentWizard/TerraformExport';
import { useDeploymentStore } from '../stores/deployment';
import { useWizardStore } from '../stores/wizard';
import { applyCloudDeploy, getCloudDeploy, startCloudDeploy, startDeployment, type CloudDeployJob } from '../lib/deploy-api';
import { downloadStack } from '../lib/migrate-api';
import { createPendingStack } from '../lib/stacks-api';
import { buttonVariants } from '../lib/button-variants';
import { toast } from 'sonner';
import type { DeploymentOption } from '../components/DeploymentWizard/TargetSelector';
import type { Provider } from '../lib/providers-api';

type Step = 'target' | 'config' | 'deploy';
type CloudStep = 'compare' | 'configure' | 'export';
type CloudProvider = 'hetzner' | 'scaleway' | 'ovh';

const isCloudProvider = (provider: Provider): provider is CloudProvider =>
  provider === 'hetzner' || provider === 'scaleway' || provider === 'ovh';

const saveBlob = (blob: Blob, filename: string) => {
  const url = window.URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  window.URL.revokeObjectURL(url);
  document.body.removeChild(a);
};

export function Deploy() {
  const [step, setStep] = useState<Step>('target');
  const [cloudStep, setCloudStep] = useState<CloudStep>('compare');
  const [selectedCloudProvider, setSelectedCloudProvider] = useState<CloudProvider | null>(null);
  const [selectedCloudBaseCost, setSelectedCloudBaseCost] = useState(0);
  const [cloudJob, setCloudJob] = useState<CloudDeployJob | null>(null);
  const {
    target,
    reset,
    setTarget,
    setCloudProvider,
    setDeploymentId,
    getConfig,
    localConfig,
    sshConfig,
    cloudConfig,
  } = useDeploymentStore();
  const { analysisResult, selectedResources } = useWizardStore();
  const navigate = useNavigate();
  const activeStep = step === 'config' && !target ? 'target' : step;
  const cloudResources = selectedResources;
  const cloudMappingResults = {
    resources: cloudResources,
    warnings: analysisResult?.warnings ?? [],
    provider: analysisResult?.provider ?? 'unknown',
  };

  const saveMutation = useMutation({
    mutationFn: async () => {
      // Get stack name from config
      const stackName = target === 'local'
        ? localConfig.projectName
        : target === 'ssh'
          ? sshConfig.projectName
          : cloudConfig.domain || `${cloudConfig.provider ?? 'cloud'}-deployment`;

      if (!stackName) {
        throw new Error('Project name is required');
      }

      const deploymentConfig = {
        provider: cloudConfig.provider ?? 'hetzner',
        region: cloudConfig.region?.id ?? 'fsn1',
        ha_level: cloudConfig.haLevel,
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
      if (!config || !target || target === 'cloud') {
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

  const handleTargetSelect = (selectedTarget: DeploymentOption) => {
    if (selectedTarget === 'cloud') {
      if (cloudResources.length === 0) {
        toast.error('Select resources in Migrate before exporting cloud Terraform.');
        return;
      }
      setTarget('cloud');
      setCloudStep('compare');
      setSelectedCloudProvider(null);
      setStep('config');
      return;
    }

    if (selectedTarget === 'export') {
      void handleDockerExport();
      return;
    }

    setTarget(selectedTarget);
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
    setCloudStep('compare');
    setSelectedCloudProvider(null);
    setSelectedCloudBaseCost(0);
    setCloudJob(null);
    setStep('target');
  };

  const handleCloudProviderSelect = (provider: Provider, baseCost: number) => {
    if (!isCloudProvider(provider)) {
      return;
    }

    setCloudProvider(provider);
    setSelectedCloudProvider(provider);
    setSelectedCloudBaseCost(baseCost);
    setCloudJob(null);
    setCloudStep('configure');
  };

  const handleCloudExport = () => {
    if (!cloudConfig.region) {
      toast.error('Please select a cloud region before exporting.');
      return;
    }
    if (cloudResources.length === 0) {
      toast.error('Select resources in Migrate before exporting cloud Terraform.');
      return;
    }
    setCloudStep('export');
  };

  const cloudProjectName = cloudConfig.domain || 'homeport-cloud';

  const pollCloudJob = async (id: string): Promise<CloudDeployJob> => {
    for (;;) {
      const job = await getCloudDeploy(id);
      setCloudJob(job);
      if (job.status === 'planned' || job.status === 'applied' || job.status === 'failed') {
        return job;
      }
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
  };

  const cloudDeployMutation = useMutation({
    mutationFn: async (apply: boolean) => {
      if (!selectedCloudProvider || !cloudConfig.region) {
        throw new Error('Cloud provider and region are required');
      }
      const job = await startCloudDeploy({
        resources: cloudResources,
        config: {
          provider: selectedCloudProvider,
          project_name: cloudProjectName,
          domain: cloudConfig.domain,
          region: cloudConfig.region.id,
        },
        apply,
      });
      setCloudJob(job);
      return pollCloudJob(job.id);
    },
    onSuccess: (job) => {
      setCloudJob(job);
      if (job.status === 'failed') {
        toast.error(job.error || 'Terraform job failed');
      } else if (job.status === 'planned') {
        toast.success('Terraform plan completed');
      } else if (job.status === 'applied') {
        toast.success('Terraform apply completed');
      }
    },
    onError: (error: Error) => {
      toast.error(error.message);
    },
  });

  const handleDockerExport = async () => {
    if (selectedResources.length === 0) {
      toast.error('Select resources in Migrate before exporting Docker ZIP.');
      return;
    }
    try {
      const blob = await downloadStack(selectedResources, {
        domain: cloudConfig.domain || 'homeport.local',
        consolidate: true,
        include_migration: true,
        include_monitoring: true,
        ha: false,
      });
      saveBlob(blob, 'homeport-docker-stack.zip');
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Export failed');
    }
  };

  const handleCloudPlan = () => cloudDeployMutation.mutate(false);
  const handleCloudApply = async () => {
    if (!cloudJob?.id) return;
    try {
      setCloudJob(await applyCloudDeploy(cloudJob.id));
      const job = await pollCloudJob(cloudJob.id);
      if (job.status === 'applied') {
        toast.success('Terraform apply completed');
      } else if (job.status === 'failed') {
        toast.error(job.error || 'Terraform job failed');
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : 'Terraform apply failed');
    }
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
          <p className="text-muted-foreground">Deploy your self-hosted stack to local Docker, remote servers, or EU cloud providers</p>
        </div>
        {activeStep !== 'target' && (
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
        <div className={activeStep === 'target' ? 'text-primary font-medium' : 'text-muted-foreground'}>
          1. Select Target
        </div>
        <div className="h-px w-8 bg-border" />
        <div className={activeStep === 'config' ? 'text-primary font-medium' : 'text-muted-foreground'}>
          2. Configure
        </div>
        <div className="h-px w-8 bg-border" />
        <div className={activeStep === 'deploy' ? 'text-primary font-medium' : 'text-muted-foreground'}>
          3. Deploy
        </div>
      </div>

      {/* Step Content */}
      {activeStep === 'target' && <TargetSelector onSelect={handleTargetSelect} />}
      {activeStep === 'config' && target === 'cloud' && cloudStep === 'compare' && (
        <ProviderComparison
          mappingResults={cloudMappingResults}
          onSelect={handleCloudProviderSelect}
          onBack={() => setStep('target')}
        />
      )}
      {activeStep === 'config' && target === 'cloud' && cloudStep === 'configure' && selectedCloudProvider && (
        <>
          <ProviderConfigForm
            provider={selectedCloudProvider}
            baseCost={selectedCloudBaseCost}
            onBack={() => setCloudStep('compare')}
            onDeploy={handleCloudPlan}
            deployLabel={cloudDeployMutation.isPending ? 'Running Terraform...' : 'Plan Terraform Deploy'}
          />
          <div className="flex flex-wrap items-center gap-3 rounded-lg border p-4">
            <button
              onClick={handleCloudExport}
              disabled={!cloudConfig.region}
              className={buttonVariants({ variant: 'outline' })}
            >
              Download Terraform ZIP
            </button>
            {cloudJob?.status === 'planned' && (
              <button
                onClick={handleCloudApply}
                disabled={cloudDeployMutation.isPending}
                className={buttonVariants({ variant: 'primary' })}
              >
                Apply Terraform
              </button>
            )}
            {cloudJob && (
              <span className="text-sm text-muted-foreground">
                Terraform job {cloudJob.status}{cloudJob.error ? `: ${cloudJob.error}` : ''}
              </span>
            )}
          </div>
        </>
      )}
      {activeStep === 'config' && target === 'cloud' && cloudStep === 'export' && selectedCloudProvider && cloudConfig.region && (
        <TerraformExport
          provider={selectedCloudProvider}
          resources={cloudResources}
          config={{
            project_name: cloudProjectName,
            domain: cloudConfig.domain,
            region: cloudConfig.region.id,
          }}
          onBack={() => setCloudStep('configure')}
        />
      )}
      {activeStep === 'config' && target !== 'cloud' && target && (
        <ConfigurationForm
          onBack={() => setStep('target')}
          onDeploy={handleDeploy}
          onSaveForLater={handleSaveForLater}
          isDeploying={deployMutation.isPending}
          isSaving={saveMutation.isPending}
        />
      )}
      {activeStep === 'deploy' && (
        <DeploymentExecution
          onRetry={handleRetry}
          onComplete={handleComplete}
        />
      )}
    </div>
  );
}
