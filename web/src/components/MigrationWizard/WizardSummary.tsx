import { CheckCircle2, Circle, CircleDot } from 'lucide-react';
import { STEP_LABELS, type WizardStep } from '@/stores/wizard';
import { cn } from '@/lib/utils';

interface WizardSummaryProps {
  steps: WizardStep[];
  currentStep: WizardStep;
  completedSteps: WizardStep[];
}

export function WizardSummary({ steps, currentStep, completedSteps }: WizardSummaryProps) {
  return (
    <div className="mt-4 grid gap-2 text-sm md:grid-cols-3 lg:grid-cols-6">
      {steps.map((step) => {
        const done = completedSteps.includes(step) || (currentStep === 'done' && step === 'done');
        const active = step === currentStep;
        const Icon = done ? CheckCircle2 : active ? CircleDot : Circle;
        return (
          <div
            key={step}
            className={cn(
              'flex items-center gap-2 rounded-md border px-3 py-2',
              active && 'border-primary bg-primary/5',
              done && !active && 'border-accent/40 bg-accent/5'
            )}
          >
            <Icon className={cn('h-4 w-4', done ? 'text-accent' : active ? 'text-primary' : 'text-muted-foreground')} />
            <span className="truncate">{STEP_LABELS[step]}</span>
          </div>
        );
      })}
    </div>
  );
}
