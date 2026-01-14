import { Check, Circle, Loader2 } from 'lucide-react';
import type { PhaseEvent } from '@/lib/deploy-api';

interface DeploymentProgressProps {
  phases: string[];
  currentPhase: PhaseEvent | null;
  isComplete: boolean;
  isFailed: boolean;
}

export function DeploymentProgress({ phases, currentPhase, isComplete, isFailed }: DeploymentProgressProps) {
  return (
    <div className="space-y-2">
      {phases.map((phase, index) => {
        const phaseIndex = index + 1;
        const isCurrent = currentPhase?.index === phaseIndex;
        const isCompleted = currentPhase ? phaseIndex < currentPhase.index : false;

        const bgClass = isCurrent ? 'bg-success/10' : '';
        const textClass = isCurrent ? 'font-medium text-success' : isCompleted ? 'text-muted-foreground' : 'text-muted-foreground/50';

        return (
          <div key={phase} className={"flex items-center gap-3 p-2 rounded-lg " + bgClass}>
            <div className="flex-shrink-0">
              {isCompleted || (isComplete && !isFailed) ? (
                <div className="w-6 h-6 rounded-full bg-success flex items-center justify-center">
                  <Check className="w-4 h-4 text-white" />
                </div>
              ) : isCurrent ? (
                <div className="w-6 h-6 rounded-full bg-success flex items-center justify-center">
                  {isFailed ? (
                    <span className="text-white text-xs font-bold">!</span>
                  ) : (
                    <Loader2 className="w-4 h-4 text-white animate-spin" />
                  )}
                </div>
              ) : (
                <div className="w-6 h-6 rounded-full border-2 border-muted-foreground/30 flex items-center justify-center">
                  <Circle className="w-3 h-3 text-muted-foreground/30" />
                </div>
              )}
            </div>
            <span className={"text-sm " + textClass}>{phase}</span>
          </div>
        );
      })}
    </div>
  );
}
