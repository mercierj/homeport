import { useState, useEffect, useRef } from 'react';
import {
  Server,
  Globe,
  Key,
  Terminal,
  CheckCircle2,
  XCircle,
  Loader2,
  Play,
  RotateCcw,
  AlertCircle,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { buttonVariants } from '@/lib/button-variants';
import { useWizardStore } from '@/stores/wizard';
import { getBundleCompose } from '@/lib/bundle-api';
import {
  startDeployment,
  subscribeToDeployment,
  cancelDeployment,
  type LocalDeployConfig,
  type SSHDeployConfig,
  type PhaseEvent,
  type LogEvent,
  type CompleteEvent,
  type ErrorEvent,
} from '@/lib/deploy-api';

// Deployment phases - these come from backend but we show placeholders
interface DeployPhase {
  id: string;
  label: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
  message?: string;
}

export function DeployStep() {
  const {
    bundleId,
    bundleManifest,
    deployTarget,
    sshConfig,
    deployProgress,
    isDeploying,
    deploymentId,
    awsCredentials,
    setDeployTarget,
    updateSSHConfig,
    setDeploymentId,
    setDeployProgress,
    setIsDeploying,
    setError,
    nextStep,
  } = useWizardStore();

  const [phases, setPhases] = useState<DeployPhase[]>([]);
  const [deployComplete, setDeployComplete] = useState(false);
  const [deployError, setDeployError] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const unsubscribeRef = useRef<(() => void) | null>(null);

  // Cleanup SSE subscription on unmount
  useEffect(() => {
    return () => {
      if (unsubscribeRef.current) {
        unsubscribeRef.current();
      }
    };
  }, []);

  const addLog = (level: string, message: string) => {
    const timestamp = new Date().toLocaleTimeString();
    const prefix = level === 'error' ? '[ERROR]' : level === 'warn' ? '[WARN]' : '[INFO]';
    setLogs((prev) => [...prev, `[${timestamp}] ${prefix} ${message}`]);
  };

  const handlePhaseEvent = (event: PhaseEvent) => {
    // Build phases array from the event
    const newPhases: DeployPhase[] = [];
    for (let i = 0; i < event.total; i++) {
      newPhases.push({
        id: `phase-${i}`,
        label: i === event.index - 1 ? event.phase : `Phase ${i + 1}`,
        status: i < event.index - 1 ? 'completed' : i === event.index - 1 ? 'running' : 'pending',
      });
    }
    // Update current phase label
    if (event.index > 0 && event.index <= newPhases.length) {
      newPhases[event.index - 1].label = event.phase;
    }
    setPhases(newPhases);
    addLog('info', event.phase);
  };

  const handleLogEvent = (event: LogEvent) => {
    addLog(event.level, event.message);
  };

  const handleCompleteEvent = (_event: CompleteEvent) => {
    setIsDeploying(false);
    setDeployComplete(true);
    setDeployProgress(100);
    // Mark all phases as completed
    setPhases((prev) => prev.map((p) => ({ ...p, status: 'completed' as const })));
    addLog('info', 'Deployment completed successfully');
  };

  const handleErrorEvent = (event: ErrorEvent) => {
    setDeployError(event.message);
    addLog('error', event.message);
    if (!event.recoverable) {
      setIsDeploying(false);
      // Mark current phase as failed
      setPhases((prev) =>
        prev.map((p) => (p.status === 'running' ? { ...p, status: 'failed' as const } : p))
      );
    }
  };

  // Start real deployment
  const handleDeploy = async () => {
    setError(null);
    setDeployError(null);

    if (!deployTarget) {
      setError('Please select a deployment target');
      return;
    }

    if (deployTarget === 'ssh' && (!sshConfig.host || !sshConfig.username)) {
      setError('Please configure SSH connection settings');
      return;
    }

    if (!bundleId) {
      setError('No bundle available. Please go back and create a bundle first.');
      return;
    }

    try {
      setIsDeploying(true);
      setDeployComplete(false);
      setLogs([]);
      setPhases([]);
      setDeployProgress(0);

      addLog('info', 'Fetching bundle compose file...');

      // Get compose content from bundle
      const composeResponse = await getBundleCompose(bundleId);
      const composeContent = composeResponse.content;

      addLog('info', 'Starting deployment...');

      // Build config based on target
      let config: LocalDeployConfig | SSHDeployConfig;

      if (deployTarget === 'local') {
        config = {
          projectName: bundleManifest?.source?.provider || 'homeport-migration',
          dataDirectory: '',
          networkMode: 'bridge',
          autoStart: true,
          enableMonitoring: false,
          composeContent,
          scripts: {},
          runtime: 'auto',
          // Include AWS credentials for data migration if available
          ...(awsCredentials.accessKeyId && {
            awsAccessKeyId: awsCredentials.accessKeyId,
            awsSecretAccessKey: awsCredentials.secretAccessKey,
          }),
        } as LocalDeployConfig;
      } else {
        config = {
          host: sshConfig.host,
          port: sshConfig.port,
          username: sshConfig.username,
          authMethod: sshConfig.authMethod,
          keyPath: sshConfig.keyPath || '',
          password: sshConfig.password || '',
          remoteDir: '/opt/homeport',
          composeContent,
          scripts: {},
          projectName: bundleManifest?.source?.provider || 'homeport-migration',
          runtime: 'auto',
        } as SSHDeployConfig;
      }

      // Start deployment
      const response = await startDeployment(deployTarget, config);
      setDeploymentId(response.deployment_id);

      addLog('info', `Deployment started with ID: ${response.deployment_id}`);

      // Subscribe to SSE for real-time updates
      unsubscribeRef.current = subscribeToDeployment(response.deployment_id, {
        onPhase: handlePhaseEvent,
        onProgress: (event) => setDeployProgress(event.percent),
        onLog: handleLogEvent,
        onComplete: handleCompleteEvent,
        onError: handleErrorEvent,
        onClose: () => {
          // Connection closed - check if we're still deploying
          if (!deployComplete && isDeploying) {
            addLog('warn', 'Connection to deployment stream closed');
          }
        },
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Deployment failed';
      setError(message);
      setDeployError(message);
      setIsDeploying(false);
      addLog('error', message);
    }
  };

  // Cancel deployment
  const handleCancel = async () => {
    if (deploymentId) {
      try {
        await cancelDeployment(deploymentId);
        addLog('info', 'Deployment cancelled');
      } catch (err) {
        addLog('error', 'Failed to cancel deployment');
      }
    }
    if (unsubscribeRef.current) {
      unsubscribeRef.current();
    }
    setIsDeploying(false);
  };

  // Handle retry
  const handleRetry = () => {
    if (unsubscribeRef.current) {
      unsubscribeRef.current();
    }
    setPhases([]);
    setDeployComplete(false);
    setDeployError(null);
    setDeployProgress(0);
    setLogs([]);
    setDeploymentId(null);
  };

  return (
    <div className="space-y-6">
      {/* Target selection */}
      {!isDeploying && !deployComplete && (
        <>
          <div>
            <h3 className="text-lg font-semibold mb-2">Select Deployment Target</h3>
            <p className="text-muted-foreground">
              Choose where to deploy your Docker stack. You can deploy locally
              or to a remote server via SSH.
            </p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {/* Local deployment */}
            <button
              onClick={() => setDeployTarget('local')}
              className={cn(
                'card-action p-6 text-left',
                deployTarget === 'local' && 'card-action-active border-primary'
              )}
            >
              <div className="flex items-start gap-4">
                <div className="p-3 rounded-lg bg-primary/10">
                  <Server className="w-6 h-6 text-primary" />
                </div>
                <div>
                  <h4 className="font-semibold">Local Deployment</h4>
                  <p className="text-sm text-muted-foreground mt-1">
                    Deploy to this machine using Docker Compose
                  </p>
                </div>
              </div>
            </button>

            {/* Remote deployment */}
            <button
              onClick={() => setDeployTarget('ssh')}
              className={cn(
                'card-action p-6 text-left',
                deployTarget === 'ssh' && 'card-action-active border-primary'
              )}
            >
              <div className="flex items-start gap-4">
                <div className="p-3 rounded-lg bg-accent/10">
                  <Globe className="w-6 h-6 text-accent" />
                </div>
                <div>
                  <h4 className="font-semibold">Remote Server (SSH)</h4>
                  <p className="text-sm text-muted-foreground mt-1">
                    Deploy to a remote server via secure SSH connection
                  </p>
                </div>
              </div>
            </button>
          </div>

          {/* SSH configuration */}
          {deployTarget === 'ssh' && (
            <div className="bg-card border border-border rounded-lg p-4 space-y-4">
              <h4 className="font-medium flex items-center gap-2">
                <Key className="w-4 h-4" />
                SSH Connection Settings
              </h4>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                <div>
                  <label className="label">Host</label>
                  <input
                    type="text"
                    value={sshConfig.host}
                    onChange={(e) => updateSSHConfig({ host: e.target.value })}
                    className="input"
                    placeholder="192.168.1.100 or server.example.com"
                  />
                </div>
                <div>
                  <label className="label">Port</label>
                  <input
                    type="number"
                    value={sshConfig.port}
                    onChange={(e) => updateSSHConfig({ port: parseInt(e.target.value) || 22 })}
                    className="input"
                    placeholder="22"
                  />
                </div>
                <div>
                  <label className="label">Username</label>
                  <input
                    type="text"
                    value={sshConfig.username}
                    onChange={(e) => updateSSHConfig({ username: e.target.value })}
                    className="input"
                    placeholder="root"
                  />
                </div>
                <div>
                  <label className="label">Authentication</label>
                  <select
                    value={sshConfig.authMethod}
                    onChange={(e) => updateSSHConfig({ authMethod: e.target.value as 'key' | 'password' })}
                    className="select"
                  >
                    <option value="key">SSH Key</option>
                    <option value="password">Password</option>
                  </select>
                </div>

                {sshConfig.authMethod === 'key' && (
                  <div className="md:col-span-2">
                    <label className="label">Key Path</label>
                    <input
                      type="text"
                      value={sshConfig.keyPath}
                      onChange={(e) => updateSSHConfig({ keyPath: e.target.value })}
                      className="input"
                      placeholder="~/.ssh/id_rsa"
                    />
                  </div>
                )}

                {sshConfig.authMethod === 'password' && (
                  <div className="md:col-span-2">
                    <label className="label">Password</label>
                    <input
                      type="password"
                      value={sshConfig.password}
                      onChange={(e) => updateSSHConfig({ password: e.target.value })}
                      className="input"
                      placeholder="SSH password"
                    />
                  </div>
                )}
              </div>
            </div>
          )}

          {/* Deploy button */}
          {deployTarget && (
            <div className="flex justify-center pt-4">
              <button
                onClick={handleDeploy}
                className={cn(buttonVariants({ variant: 'primary', size: 'lg' }), 'gap-2')}
              >
                <Play className="w-5 h-5" />
                Start Deployment
              </button>
            </div>
          )}
        </>
      )}

      {/* Deployment progress */}
      {(isDeploying || deployComplete) && (
        <div className="space-y-6">
          <div className="flex items-center justify-between">
            <div>
              <h3 className="text-lg font-semibold">
                {deployComplete ? 'Deployment Complete' : deployError ? 'Deployment Failed' : 'Deploying...'}
              </h3>
              <p className="text-muted-foreground">
                {deployComplete
                  ? 'Your stack has been successfully deployed'
                  : deployError
                  ? deployError
                  : `Deploying to ${deployTarget === 'local' ? 'local machine' : sshConfig.host}`}
              </p>
            </div>
            {deployComplete && (
              <CheckCircle2 className="w-8 h-8 text-accent" />
            )}
            {deployError && !isDeploying && (
              <AlertCircle className="w-8 h-8 text-error" />
            )}
          </div>

          {/* Progress bar */}
          <div className="space-y-2">
            <div className="flex items-center justify-between text-sm">
              <span>Progress</span>
              <span className="font-medium">{deployProgress}%</span>
            </div>
            <div className="progress h-3">
              <div
                className="progress-indicator transition-all duration-300"
                style={{ width: `${deployProgress}%` }}
              />
            </div>
          </div>

          {/* Phases */}
          {phases.length > 0 && (
            <div className="space-y-2">
              {phases.map((phase) => (
                <div
                  key={phase.id}
                  className={cn(
                    'flex items-center gap-3 p-3 rounded-lg',
                    phase.status === 'running' && 'bg-primary/5 border border-primary/20',
                    phase.status === 'completed' && 'bg-accent/5',
                    phase.status === 'failed' && 'bg-error/5'
                  )}
                >
                  {phase.status === 'pending' && (
                    <div className="w-5 h-5 rounded-full border-2 border-muted-foreground/30" />
                  )}
                  {phase.status === 'running' && (
                    <Loader2 className="w-5 h-5 text-primary animate-spin" />
                  )}
                  {phase.status === 'completed' && (
                    <CheckCircle2 className="w-5 h-5 text-accent" />
                  )}
                  {phase.status === 'failed' && (
                    <XCircle className="w-5 h-5 text-error" />
                  )}
                  <span
                    className={cn(
                      phase.status === 'running' && 'font-medium text-primary',
                      phase.status === 'completed' && 'text-accent',
                      phase.status === 'failed' && 'text-error',
                      phase.status === 'pending' && 'text-muted-foreground'
                    )}
                  >
                    {phase.label}
                  </span>
                </div>
              ))}
            </div>
          )}

          {/* Logs */}
          <div className="bg-muted/50 rounded-lg">
            <div className="flex items-center gap-2 px-4 py-2 border-b border-border">
              <Terminal className="w-4 h-4" />
              <span className="text-sm font-medium">Deployment Logs</span>
            </div>
            <div className="p-4 font-mono text-xs max-h-40 overflow-y-auto">
              {logs.map((log, index) => (
                <div
                  key={index}
                  className={cn(
                    'text-muted-foreground',
                    log.includes('[ERROR]') && 'text-error',
                    log.includes('[WARN]') && 'text-warning'
                  )}
                >
                  {log}
                </div>
              ))}
              {logs.length === 0 && (
                <div className="text-muted-foreground">Waiting for logs...</div>
              )}
            </div>
          </div>

          {/* Action buttons */}
          <div className="flex items-center justify-between pt-4 border-t border-border">
            {deployComplete ? (
              <>
                <button
                  onClick={handleRetry}
                  className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}
                >
                  <RotateCcw className="w-4 h-4" />
                  Deploy Again
                </button>
                <button
                  onClick={nextStep}
                  className={buttonVariants({ variant: 'primary' })}
                >
                  Continue to Sync
                </button>
              </>
            ) : deployError && !isDeploying ? (
              <>
                <button
                  onClick={handleRetry}
                  className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}
                >
                  <RotateCcw className="w-4 h-4" />
                  Try Again
                </button>
                <div />
              </>
            ) : (
              <>
                <button
                  onClick={handleCancel}
                  className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}
                >
                  Cancel
                </button>
                <div className="text-sm text-muted-foreground">
                  Deployment in progress...
                </div>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
