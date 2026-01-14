import type { ReactNode } from 'react';
import { ArrowLeft, ArrowRight, X, Upload, FolderSearch } from 'lucide-react';
import { cn } from '@/lib/utils';
import { buttonVariants } from '@/lib/button-variants';
import { WizardProgress } from './WizardProgress';
import {
  useWizardStore,
  SOURCE_WIZARD_STEPS,
  BUNDLE_WIZARD_STEPS,
  type WizardStep,
  type WizardEntryPoint,
} from '@/stores/wizard';

interface WizardContainerProps {
  children: ReactNode;
  onClose?: () => void;
  hideNavigation?: boolean;
  canProceed?: boolean;
  proceedLabel?: string;
  onProceed?: () => void;
  showBack?: boolean;
}

export function WizardContainer({
  children,
  onClose,
  hideNavigation = false,
  canProceed = true,
  proceedLabel,
  onProceed,
  showBack = true,
}: WizardContainerProps) {
  const {
    currentStep,
    completedSteps,
    entryPoint,
    goToStep,
    nextStep,
    previousStep,
    reset,
    error,
    clearError,
  } = useWizardStore();

  const handleClose = () => {
    reset();
    onClose?.();
  };

  const handleProceed = () => {
    if (onProceed) {
      onProceed();
    } else {
      nextStep();
    }
  };

  const handleStepClick = (step: WizardStep) => {
    goToStep(step);
  };

  // Get step index for navigation
  const steps = entryPoint === 'bundle' ? BUNDLE_WIZARD_STEPS : SOURCE_WIZARD_STEPS;
  const currentIndex = steps.indexOf(currentStep);
  const isFirstStep = currentIndex === 0;
  const isLastStep = currentIndex === steps.length - 1;

  // Default proceed label based on step
  const defaultProceedLabel = isLastStep
    ? 'Complete'
    : currentStep === 'analyze'
    ? 'Continue to Export'
    : currentStep === 'export'
    ? 'Export Bundle'
    : currentStep === 'upload'
    ? 'Continue to Secrets'
    : currentStep === 'secrets'
    ? 'Continue to Deploy'
    : currentStep === 'deploy'
    ? 'Deploy'
    : currentStep === 'sync'
    ? 'Start Sync'
    : currentStep === 'cutover'
    ? 'Execute Cutover'
    : 'Continue';

  return (
    <div className="flex flex-col h-full min-h-0">
      {/* Header */}
      <div className="flex-shrink-0 border-b border-border/50 bg-card/50 backdrop-blur-sm">
        <div className="px-6 py-4">
          <div className="flex items-center justify-between mb-6">
            <div>
              <h1 className="text-2xl font-bold bg-gradient-primary bg-clip-text text-transparent">
                Migration Wizard
              </h1>
              <p className="text-sm text-muted-foreground mt-1">
                Step-by-step guide to migrate your infrastructure
              </p>
            </div>
            {onClose && (
              <button
                onClick={handleClose}
                className={cn(
                  buttonVariants({ variant: 'ghost', size: 'icon' }),
                  'text-muted-foreground hover:text-foreground'
                )}
                aria-label="Close wizard"
              >
                <X className="w-5 h-5" />
              </button>
            )}
          </div>

          {/* Progress indicator */}
          <WizardProgress
            currentStep={currentStep}
            completedSteps={completedSteps}
            entryPoint={entryPoint}
            onStepClick={handleStepClick}
          />
        </div>
      </div>

      {/* Error banner */}
      {error && (
        <div className="flex-shrink-0 px-6 py-3 bg-error/10 border-b border-error/20">
          <div className="flex items-center justify-between">
            <p className="text-sm text-error">{error}</p>
            <button
              onClick={clearError}
              className="text-error hover:text-error/80 text-sm font-medium"
            >
              Dismiss
            </button>
          </div>
        </div>
      )}

      {/* Content */}
      <div className="flex-1 overflow-auto min-h-0">
        <div className="p-6">{children}</div>
      </div>

      {/* Footer with navigation */}
      {!hideNavigation && (
        <div className="flex-shrink-0 border-t border-border/50 bg-card/50 backdrop-blur-sm">
          <div className="px-6 py-4 flex items-center justify-between">
            {/* Back button */}
            <div>
              {showBack && !isFirstStep && (
                <button
                  onClick={previousStep}
                  className={cn(
                    buttonVariants({ variant: 'outline' }),
                    'gap-2'
                  )}
                >
                  <ArrowLeft className="w-4 h-4" />
                  Back
                </button>
              )}
            </div>

            {/* Proceed button */}
            <button
              onClick={handleProceed}
              disabled={!canProceed}
              className={cn(
                buttonVariants({ variant: 'primary' }),
                'gap-2',
                !canProceed && 'opacity-50 cursor-not-allowed'
              )}
            >
              {proceedLabel || defaultProceedLabel}
              {!isLastStep && <ArrowRight className="w-4 h-4" />}
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// Entry point selection component
interface WizardEntrySelectionProps {
  onSelectEntry: (entry: WizardEntryPoint) => void;
}

export function WizardEntrySelection({ onSelectEntry }: WizardEntrySelectionProps) {
  return (
    <div className="flex flex-col items-center justify-center min-h-[400px] p-8">
      <h2 className="text-2xl font-bold text-center mb-2">
        How would you like to start?
      </h2>
      <p className="text-muted-foreground text-center mb-8 max-w-lg">
        Choose to analyze your current cloud infrastructure, or import an existing
        migration bundle.
      </p>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6 max-w-2xl w-full">
        {/* Start from source */}
        <button
          onClick={() => onSelectEntry('source')}
          className={cn(
            'card-action p-6 text-left',
            'hover:border-primary/50 hover:shadow-glow-primary'
          )}
        >
          <div className="resource-icon-compute mb-4">
            <FolderSearch className="w-6 h-6" />
          </div>
          <h3 className="text-lg font-semibold mb-2">Analyze Source</h3>
          <p className="text-sm text-muted-foreground">
            Scan your Terraform files, cloud state, or live infrastructure to discover
            resources and generate a migration bundle.
          </p>
        </button>

        {/* Import existing bundle */}
        <button
          onClick={() => onSelectEntry('bundle')}
          className={cn(
            'card-action p-6 text-left',
            'hover:border-accent/50 hover:shadow-glow-success'
          )}
        >
          <div className="resource-icon-storage mb-4">
            <Upload className="w-6 h-6" />
          </div>
          <h3 className="text-lg font-semibold mb-2">Upload Bundle</h3>
          <p className="text-sm text-muted-foreground">
            Upload an existing .hprt migration bundle to continue the deployment
            process from where you left off.
          </p>
        </button>
      </div>
    </div>
  );
}
