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

export default function Migrate() {
  const navigate = useNavigate();
  const { currentStep, entryPoint, setEntryPoint, reset, bundleManifest } = useWizardStore();

  // Handle close - reset wizard state and navigate back to dashboard
  const handleClose = () => {
    reset();
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
