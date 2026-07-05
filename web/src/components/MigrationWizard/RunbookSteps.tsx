import { useEffect, useMemo, useState } from 'react';
import {
  AlertCircle,
  CheckCircle2,
  Circle,
  Loader2,
  Play,
  RotateCcw,
  XCircle,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { buttonVariants } from '@/lib/button-variants';
import {
  getRunbook,
  rollbackRunbook,
  runRunbook,
  runRunbookStep,
  type Runbook,
  type RunbookStep,
  type RunbookStepStatus,
} from '@/lib/runbook-api';

const GROUPS = ['Credentials', 'Provision', 'Sync', 'Validate', 'Cutover', 'Rollback'];

const STATUS_ICON: Record<RunbookStepStatus, typeof Circle> = {
  pending: Circle,
  running: Loader2,
  passed: CheckCircle2,
  failed: XCircle,
  skipped: CheckCircle2,
  blocked: AlertCircle,
};

interface RunbookStepsProps {
  runbookId?: string | null;
  onRequiredPassedChange?: (passed: boolean) => void;
}

export function RunbookSteps({ runbookId, onRequiredPassedChange }: RunbookStepsProps) {
  const [runbook, setRunbook] = useState<Runbook | null>(null);
  const [loadingStepId, setLoadingStepId] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const requiredPassed = useMemo(() => {
    if (!runbook) return true;
    return runbook.steps.every((step) =>
      step.optional || step.status === 'passed' || step.status === 'skipped'
    );
  }, [runbook]);

  useEffect(() => {
    onRequiredPassedChange?.(requiredPassed);
  }, [onRequiredPassedChange, requiredPassed]);

  useEffect(() => {
    if (!runbookId) {
      setRunbook(null);
      setError(null);
      return;
    }

    let cancelled = false;
    getRunbook(runbookId)
      .then((data) => {
        if (!cancelled) {
          setRunbook(data);
          setError(null);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setRunbook(null);
          setError(null);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [runbookId]);

  if (!runbook) return null;

  const refresh = async () => {
    if (!runbookId) return;
    setRunbook(await getRunbook(runbookId));
  };

  const runStep = async (step: RunbookStep) => {
    if (!runbookId) return;
    setLoadingStepId(step.id);
    setError(null);
    try {
      await runRunbookStep(runbookId, step.id);
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Step failed');
      await refresh().catch(() => undefined);
    } finally {
      setLoadingStepId(null);
    }
  };

  const runAll = async () => {
    if (!runbookId) return;
    setLoadingStepId('__all__');
    setError(null);
    try {
      setRunbook(await runRunbook(runbookId));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Runbook failed');
      await refresh().catch(() => undefined);
    } finally {
      setLoadingStepId(null);
    }
  };

  const rollback = async () => {
    if (!runbookId) return;
    setLoadingStepId('__rollback__');
    setError(null);
    try {
      setRunbook(await rollbackRunbook(runbookId));
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Rollback failed');
    } finally {
      setLoadingStepId(null);
    }
  };

  const grouped = GROUPS.map((group) => ({
    group,
    steps: runbook.steps.filter((step) => (step.group || 'Provision') === group),
  })).filter((item) => item.steps.length > 0);

  return (
    <div className="space-y-4 border border-border rounded-lg p-4">
      <div className="flex items-center justify-between gap-3">
        <div>
          <h4 className="font-medium">{runbook.name}</h4>
          <p className="text-sm text-muted-foreground">
            {runbook.steps.filter((step) => step.status === 'passed').length} of {runbook.steps.length} steps passed
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={runAll}
            disabled={loadingStepId !== null || requiredPassed}
            className={cn(buttonVariants({ variant: 'outline', size: 'sm' }), 'gap-2')}
          >
            {loadingStepId === '__all__' ? <Loader2 className="w-4 h-4 animate-spin" /> : <Play className="w-4 h-4" />}
            Run
          </button>
          <button
            onClick={rollback}
            disabled={loadingStepId !== null}
            className={cn(buttonVariants({ variant: 'outline', size: 'sm' }), 'gap-2')}
          >
            {loadingStepId === '__rollback__' ? <Loader2 className="w-4 h-4 animate-spin" /> : <RotateCcw className="w-4 h-4" />}
            Rollback
          </button>
        </div>
      </div>

      {error && (
        <div className="text-sm text-error bg-error/10 border border-error/20 rounded-md px-3 py-2">
          {error}
        </div>
      )}

      {grouped.map(({ group, steps }) => (
        <div key={group} className="space-y-2">
          <h5 className="text-xs font-semibold uppercase text-muted-foreground">{group}</h5>
          {steps.map((step) => {
            const Icon = STATUS_ICON[step.status] || Circle;
            const isRunning = step.status === 'running' || loadingStepId === step.id;
            const canRun = step.status === 'pending' || step.status === 'failed';

            return (
              <div
                key={step.id}
                className={cn(
                  'flex items-center justify-between gap-3 rounded-md border p-3',
                  step.status === 'passed' && 'border-accent/40 bg-accent/5',
                  step.status === 'failed' && 'border-error/40 bg-error/5',
                  step.status === 'blocked' && 'border-warning/40 bg-warning/5'
                )}
              >
                <div className="flex min-w-0 items-start gap-3">
                  <Icon
                    className={cn(
                      'mt-0.5 h-4 w-4 flex-shrink-0',
                      isRunning && 'animate-spin text-primary',
                      step.status === 'passed' && 'text-accent',
                      step.status === 'failed' && 'text-error',
                      step.status === 'blocked' && 'text-warning',
                      step.status === 'pending' && 'text-muted-foreground'
                    )}
                  />
                  <div className="min-w-0">
                    <p className="truncate text-sm font-medium">{step.name}</p>
                    <p className="text-xs text-muted-foreground">
                      {step.type.replace('_', ' ')} · {step.status}
                      {step.optional ? ' · optional' : ''}
                    </p>
                  </div>
                </div>
                <button
                  onClick={() => runStep(step)}
                  disabled={loadingStepId !== null || !canRun}
                  className={cn(buttonVariants({ variant: 'ghost', size: 'sm' }), 'gap-2 flex-shrink-0')}
                >
                  {isRunning ? <Loader2 className="w-4 h-4 animate-spin" /> : <Play className="w-4 h-4" />}
                  Run
                </button>
              </div>
            );
          })}
        </div>
      ))}
    </div>
  );
}
