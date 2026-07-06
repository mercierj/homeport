import { useEffect, useRef } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  WizardContainer,
  WizardEntrySelection,
} from '@/components/MigrationWizard/WizardContainer';
import { useWizardStore, type WizardEntryPoint } from '@/stores/wizard';

// Step components
import { AnalyzeStep } from '@/components/MigrationWizard/steps/AnalyzeStep';
import { ExportStep } from '@/components/MigrationWizard/steps/ExportStep';
import { BundleUploadStep } from '@/components/MigrationWizard/steps/BundleUploadStep';
import { SecretsStep } from '@/components/MigrationWizard/steps/SecretsStep';
import { DeployStep } from '@/components/MigrationWizard/steps/DeployStep';
import { SyncStep } from '@/components/MigrationWizard/steps/SyncStep';
import { CutoverStep } from '@/components/MigrationWizard/steps/CutoverStep';
import { createWizardSession, updateWizardSession, type WizardSessionStep } from '@/lib/wizard-session-api';

export default function Migrate() {
  const navigate = useNavigate();
  const {
    sessionId,
    currentStep,
    completedSteps,
    entryPoint,
    sourceProvider,
    selectedResources,
    bundleId,
    secretsResolved,
    deploymentId,
    syncPlanId,
    cutoverPlanId,
    setEntryPoint,
    setSessionId,
    setError,
    reset,
    bundleManifest,
  } = useWizardStore();
  const lastPatch = useRef('');

  useEffect(() => {
    if (entryPoint === null || sessionId) return;

    void createWizardSession()
      .then((session) => setSessionId(session.id))
      .catch((error) => setError(error instanceof Error ? error.message : 'Failed to create wizard session'));
  }, [entryPoint, sessionId, setError, setSessionId]);

  useEffect(() => {
    if (!sessionId) return;

    const current_step = (currentStep === 'upload' ? 'secrets' : currentStep) as WizardSessionStep;
    const completed_steps = completedSteps
      .filter((step) => step !== 'upload')
      .map((step) => step as WizardSessionStep);
    const patch = {
      current_step,
      completed_steps,
      source_provider: sourceProvider || undefined,
      selected_resources: selectedResources.map((resource) => resource.id),
      bundle_id: bundleId || undefined,
      secrets_resolved: secretsResolved,
      deployment_id: deploymentId || undefined,
      sync_plan_id: syncPlanId || undefined,
      cutover_id: cutoverPlanId || undefined,
    };
    const serialized = JSON.stringify(patch);
    if (serialized === lastPatch.current) return;
    lastPatch.current = serialized;

    void updateWizardSession(sessionId, patch).catch((error) =>
      setError(error instanceof Error ? error.message : 'Failed to update wizard session')
    );
  }, [
    bundleId,
    completedSteps,
    currentStep,
    cutoverPlanId,
    deploymentId,
    secretsResolved,
    selectedResources,
    sessionId,
    setError,
    sourceProvider,
    syncPlanId,
  ]);

  // Handle close - reset wizard state and navigate back to dashboard
  const handleClose = () => {
    reset();
    setSessionId(null);
    navigate('/');
  };

  // Handle entry point selection
  const handleSelectEntry = (entry: WizardEntryPoint) => {
    setEntryPoint(entry);
  };

  // Show entry selection if no entry point has been selected yet
  if (entryPoint === null) {
    return (
      <div className="h-full bg-background">
        <WizardEntrySelection onSelectEntry={handleSelectEntry} />
      </div>
    );
  }

  // Render current step
  const renderStep = () => {
    switch (currentStep) {
      case 'analyze':
        return <AnalyzeStep />;
      case 'export':
        return <ExportStep />;
      case 'upload':
        return <BundleUploadStep />;
      case 'secrets':
        return <SecretsStep />;
      case 'deploy':
        return <DeployStep />;
      case 'sync':
        return <SyncStep />;
      case 'cutover':
        return <CutoverStep />;
      default:
        return <AnalyzeStep />;
    }
  };

  // Determine if the user can proceed from the current step
  const canProceed = currentStep === 'upload' ? !!bundleManifest : true;

  // Steps that have their own navigation buttons (with custom validation/state)
  const stepsWithOwnNavigation = ['analyze', 'export', 'secrets', 'deploy', 'sync', 'cutover'];
  const hideNavigation = stepsWithOwnNavigation.includes(currentStep);

  return (
    <div className="h-full bg-background">
      <WizardContainer onClose={handleClose} canProceed={canProceed} hideNavigation={hideNavigation}>
        {renderStep()}
      </WizardContainer>
    </div>
  );
}
