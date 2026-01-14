import { useEffect } from 'react';
import { useDeploymentStore } from '@/stores/deployment';
import { DeploymentProgress } from './DeploymentProgress';
import { DeploymentLogs } from './DeploymentLogs';
import { subscribeToDeployment, cancelDeployment } from '@/lib/deploy-api';
import { X, RotateCcw, CheckCircle, XCircle } from 'lucide-react';

interface DeploymentExecutionProps {
  onRetry: () => void;
  onComplete: () => void;
}

const LOCAL_PHASES = [
  'Generating configuration files',
  'Creating Docker network',
  'Pulling images',
  'Starting containers',
  'Running health checks',
];

const SSH_PHASES = [
  'Connecting to server',
  'Checking Docker installation',
  'Transferring files',
  'Pulling images',
  'Starting containers',
  'Running health checks',
];

export function DeploymentExecution({ onRetry, onComplete }: DeploymentExecutionProps) {
  const {
    deploymentId,
    target,
    status,
    currentPhase,
    progress,
    logs,
    services,
    error,
    setPhase,
    setProgress,
    addLog,
    setServices,
    setError,
    setStatus,
  } = useDeploymentStore();

  const phases = target === 'ssh' ? SSH_PHASES : LOCAL_PHASES;
  const isComplete = status === 'completed';
  const isFailed = status === 'failed';
  const isCancelled = status === 'cancelled';

  useEffect(() => {
    if (!deploymentId) return;

    const unsubscribe = subscribeToDeployment(deploymentId, {
      onPhase: setPhase,
      onProgress: (event) => setProgress(event.percent),
      onLog: addLog,
      onComplete: (data) => {
        setServices(data.services);
        setStatus('completed');
      },
      onError: (data) => {
        setError(data.message);
        if (!data.recoverable) {
          setStatus('failed');
        }
      },
      onClose: () => {},
    });

    return () => {
      unsubscribe();
    };
  }, [deploymentId, setPhase, setProgress, addLog, setServices, setError, setStatus]);

  const handleCancel = async () => {
    if (deploymentId) {
      try {
        await cancelDeployment(deploymentId);
        setStatus('cancelled');
      } catch (err) {
        console.error('Failed to cancel:', err);
      }
    }
  };

  const progressBarClass = isFailed ? 'progress-error' : 'progress-success';

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-bold">
          {isComplete
            ? 'Deployment Complete'
            : isFailed
            ? 'Deployment Failed'
            : isCancelled
            ? 'Deployment Cancelled'
            : 'Deploying Stack'}
        </h2>
        {!isComplete && !isFailed && !isCancelled && (
          <button
            onClick={handleCancel}
            className="px-3 py-1.5 text-muted-foreground hover:text-foreground hover:bg-muted rounded-lg flex items-center gap-1"
          >
            <X className="h-4 w-4" />
            Cancel
          </button>
        )}
      </div>

      <div className="space-y-2">
        <div className="flex justify-between text-sm text-muted-foreground">
          <span>{currentPhase?.phase || 'Starting...'}</span>
          <span>{progress}%</span>
        </div>
        <div className="h-2 bg-muted rounded-full overflow-hidden">
          <div
            className={"h-full transition-all duration-300 " + progressBarClass}
            style={{ width: progress + '%' }}
          />
        </div>
      </div>

      <div className="grid grid-cols-2 gap-6">
        <DeploymentProgress
          phases={phases}
          currentPhase={currentPhase}
          isComplete={isComplete}
          isFailed={isFailed}
        />
        <div>
          <h3 className="text-sm font-medium mb-2">Logs</h3>
          <DeploymentLogs logs={logs} />
        </div>
      </div>

      {isComplete && (
        <div className="alert-success">
          <div className="flex items-center gap-2 font-medium mb-2">
            <CheckCircle className="h-5 w-5" />
            Deployment Successful
          </div>
          {services.length > 0 && (
            <div className="space-y-1 text-sm">
              <p>Running services:</p>
              <ul className="list-disc list-inside opacity-90">
                {services.map((svc) => (
                  <li key={svc.name}>
                    {svc.name} {svc.healthy ? '(healthy)' : '(unhealthy)'}
                    {svc.ports.length > 0 && ' - ' + svc.ports.join(', ')}
                  </li>
                ))}
              </ul>
            </div>
          )}
          <button
            onClick={onComplete}
            className="mt-4 px-4 py-2 bg-success text-success-foreground rounded-lg hover:bg-success/90"
          >
            Done
          </button>
        </div>
      )}

      {isFailed && (
        <div className="alert-error">
          <div className="flex items-center gap-2 font-medium mb-2">
            <XCircle className="h-5 w-5" />
            Deployment Failed
          </div>
          {error && <p className="text-sm opacity-90 mb-4">{error}</p>}
          <div className="flex gap-3">
            <button
              onClick={onRetry}
              className="px-4 py-2 bg-error text-error-foreground rounded-lg hover:bg-error/90 flex items-center gap-2"
            >
              <RotateCcw className="h-4 w-4" />
              Retry
            </button>
            <button onClick={onComplete} className="px-4 py-2 text-muted-foreground hover:text-foreground">
              Cancel
            </button>
          </div>
        </div>
      )}

      {isCancelled && (
        <div className="p-4 bg-muted border border-muted-foreground/20 rounded-lg">
          <p className="text-muted-foreground">Deployment was cancelled.</p>
          <button
            onClick={onComplete}
            className="mt-4 px-4 py-2 bg-muted-foreground text-background rounded-lg hover:bg-muted-foreground/90"
          >
            Back
          </button>
        </div>
      )}
    </div>
  );
}
