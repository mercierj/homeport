import { Check } from 'lucide-react';
import { cn } from '@/lib/utils';
import {
  SOURCE_WIZARD_STEPS,
  BUNDLE_WIZARD_STEPS,
  STEP_LABELS,
  STEP_DESCRIPTIONS,
  type WizardStep,
  type WizardEntryPoint,
} from '@/stores/wizard';

interface WizardProgressProps {
  currentStep: WizardStep;
  completedSteps: WizardStep[];
  entryPoint: WizardEntryPoint;
  onStepClick?: (step: WizardStep) => void;
}

export function WizardProgress({
  currentStep,
  completedSteps,
  entryPoint,
  onStepClick,
}: WizardProgressProps) {
  // Get steps based on entry point
  const visibleSteps = entryPoint === 'bundle' ? BUNDLE_WIZARD_STEPS : SOURCE_WIZARD_STEPS;

  const currentIndex = visibleSteps.indexOf(currentStep);

  return (
    <div className="w-full">
      {/* Desktop view - horizontal */}
      <div className="hidden md:block">
        <div className="flex items-center justify-between">
          {visibleSteps.map((step, index) => {
            const isCompleted = completedSteps.includes(step);
            const isCurrent = step === currentStep;
            const isPast = index < currentIndex;
            const isClickable = isCompleted || isPast;

            return (
              <div key={step} className="flex items-center flex-1">
                {/* Step circle and label */}
                <div className="flex flex-col items-center">
                  <button
                    onClick={() => isClickable && onStepClick?.(step)}
                    disabled={!isClickable}
                    className={cn(
                      'w-10 h-10 rounded-full flex items-center justify-center',
                      'text-sm font-medium transition-all duration-200',
                      'focus:outline-none focus-ring',
                      isCompleted && 'bg-accent text-white',
                      isCurrent && !isCompleted && 'bg-primary text-white ring-4 ring-primary/20',
                      !isCompleted && !isCurrent && 'bg-muted text-muted-foreground',
                      isClickable && 'cursor-pointer hover:scale-105',
                      !isClickable && 'cursor-default'
                    )}
                    aria-current={isCurrent ? 'step' : undefined}
                  >
                    {isCompleted ? (
                      <Check className="w-5 h-5" />
                    ) : (
                      <span>{index + 1}</span>
                    )}
                  </button>
                  <span
                    className={cn(
                      'mt-2 text-sm font-medium',
                      isCurrent && 'text-foreground',
                      isCompleted && 'text-accent',
                      !isCurrent && !isCompleted && 'text-muted-foreground'
                    )}
                  >
                    {STEP_LABELS[step]}
                  </span>
                </div>

                {/* Connector line */}
                {index < visibleSteps.length - 1 && (
                  <div
                    className={cn(
                      'flex-1 h-0.5 mx-4',
                      index < currentIndex ? 'bg-accent' : 'bg-muted'
                    )}
                  />
                )}
              </div>
            );
          })}
        </div>

        {/* Current step description */}
        <div className="mt-6 text-center">
          <p className="text-muted-foreground">
            {STEP_DESCRIPTIONS[currentStep]}
          </p>
        </div>
      </div>

      {/* Mobile view - vertical */}
      <div className="md:hidden space-y-4">
        {visibleSteps.map((step, index) => {
          const isCompleted = completedSteps.includes(step);
          const isCurrent = step === currentStep;
          const isPast = index < currentIndex;
          const isClickable = isCompleted || isPast;

          return (
            <div
              key={step}
              className={cn(
                'flex items-center gap-4 p-3 rounded-lg transition-all',
                isCurrent && 'bg-primary/5 border border-primary/20',
                isCompleted && 'bg-accent/5',
                !isCurrent && !isCompleted && 'opacity-60'
              )}
            >
              <button
                onClick={() => isClickable && onStepClick?.(step)}
                disabled={!isClickable}
                className={cn(
                  'w-8 h-8 rounded-full flex items-center justify-center',
                  'text-sm font-medium flex-shrink-0',
                  isCompleted && 'bg-accent text-white',
                  isCurrent && !isCompleted && 'bg-primary text-white',
                  !isCompleted && !isCurrent && 'bg-muted text-muted-foreground',
                  isClickable && 'cursor-pointer'
                )}
              >
                {isCompleted ? (
                  <Check className="w-4 h-4" />
                ) : (
                  <span>{index + 1}</span>
                )}
              </button>
              <div className="flex-1 min-w-0">
                <p
                  className={cn(
                    'font-medium',
                    isCurrent && 'text-foreground',
                    isCompleted && 'text-accent',
                    !isCurrent && !isCompleted && 'text-muted-foreground'
                  )}
                >
                  {STEP_LABELS[step]}
                </p>
                <p className="text-sm text-muted-foreground truncate">
                  {STEP_DESCRIPTIONS[step]}
                </p>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
