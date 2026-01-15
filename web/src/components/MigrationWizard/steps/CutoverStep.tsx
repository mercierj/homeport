import { useState, useEffect, useRef } from 'react';
import {
  Globe,
  CheckCircle2,
  XCircle,
  AlertCircle,
  Loader2,
  Play,
  RotateCcw,
  Shield,
  ArrowRight,
} from 'lucide-react';
import { cn } from '@/lib/utils';
import { buttonVariants } from '@/lib/button-variants';
import { useWizardStore } from '@/stores/wizard';
import {
  startCutover,
  subscribeToCutover,
  cancelCutover,
  rollbackCutover,
  type CreateCutoverRequest,
  type HealthCheckRequest,
  type DNSChangeRequest,
  type CutoverEvent,
} from '@/lib/cutover-api';

interface DNSChange {
  id: string;
  domain: string;
  recordType: string;
  oldValue: string;
  newValue: string;
  status: 'pending' | 'running' | 'completed' | 'failed';
}

interface HealthCheck {
  id: string;
  name: string;
  endpoint: string;
  type: 'pre' | 'post';
  status: 'pending' | 'running' | 'passed' | 'failed';
  error?: string;
}

export function CutoverStep() {
  const { bundleId, setError, nextStep } = useWizardStore();

  const [dnsChanges, setDNSChanges] = useState<DNSChange[]>([]);
  const [healthChecks, setHealthChecks] = useState<HealthCheck[]>([]);
  const [isCuttingOver, setIsCuttingOver] = useState(false);
  const [cutoverComplete, setCutoverComplete] = useState(false);
  const [cutoverError, setCutoverError] = useState<string | null>(null);
  const [currentPhase, setCurrentPhase] = useState<'pre_check' | 'dns' | 'post_check' | 'done'>('pre_check');
  const [cutoverId, setCutoverId] = useState<string | null>(null);
  const [isDryRun, setIsDryRun] = useState(true);
  const [logs, setLogs] = useState<string[]>([]);
  const unsubscribeRef = useRef<(() => void) | null>(null);

  // Build DNS changes and health checks
  // Currently returns empty - cutover config would come from bundle or user input
  const buildFromManifest = (): { changes: DNSChange[]; checks: HealthCheck[] } => {
    // Cutover configuration is not in the bundle yet
    // Users can skip this step or we could add manual DNS entry
    return { changes: [], checks: [] };
  };

  // Cleanup SSE subscription on unmount
  useEffect(() => {
    return () => {
      if (unsubscribeRef.current) {
        unsubscribeRef.current();
      }
    };
  }, []);

  const addLog = (message: string) => {
    const timestamp = new Date().toLocaleTimeString();
    setLogs((prev) => [...prev, `[${timestamp}] ${message}`]);
  };

  // Handle SSE events
  const handleStepStart = (event: CutoverEvent) => {
    addLog(`Starting: ${event.description}`);

    if (event.step_type === 'pre_check') {
      setCurrentPhase('pre_check');
      const checkId = healthChecks.find(c => c.type === 'pre' && c.status === 'pending')?.id;
      if (checkId) {
        setHealthChecks((prev) =>
          prev.map((c) => (c.id === checkId ? { ...c, status: 'running' } : c))
        );
      }
    } else if (event.step_type === 'dns_change') {
      setCurrentPhase('dns');
      const dnsItem = dnsChanges.find(d => d.status === 'pending');
      if (dnsItem) {
        setDNSChanges((prev) =>
          prev.map((d) => (d.id === dnsItem.id ? { ...d, status: 'running' } : d))
        );
      }
    } else if (event.step_type === 'post_check') {
      setCurrentPhase('post_check');
      const checkId = healthChecks.find(c => c.type === 'post' && c.status === 'pending')?.id;
      if (checkId) {
        setHealthChecks((prev) =>
          prev.map((c) => (c.id === checkId ? { ...c, status: 'running' } : c))
        );
      }
    }
  };

  const handleStepComplete = (event: CutoverEvent) => {
    addLog(`Completed: ${event.description}`);

    if (event.step_type === 'pre_check' || event.step_type === 'post_check') {
      const runningCheck = healthChecks.find(c => c.status === 'running');
      if (runningCheck) {
        setHealthChecks((prev) =>
          prev.map((c) => (c.id === runningCheck.id ? { ...c, status: 'passed' } : c))
        );
      }
    } else if (event.step_type === 'dns_change') {
      const runningDns = dnsChanges.find(d => d.status === 'running');
      if (runningDns) {
        setDNSChanges((prev) =>
          prev.map((d) => (d.id === runningDns.id ? { ...d, status: 'completed' } : d))
        );
      }
    }
  };

  const handleStepFailed = (event: CutoverEvent) => {
    addLog(`Failed: ${event.description} - ${event.error}`);
    setCutoverError(event.error || 'Step failed');

    if (event.step_type === 'pre_check' || event.step_type === 'post_check') {
      const runningCheck = healthChecks.find(c => c.status === 'running');
      if (runningCheck) {
        setHealthChecks((prev) =>
          prev.map((c) => (c.id === runningCheck.id ? { ...c, status: 'failed', error: event.error } : c))
        );
      }
    } else if (event.step_type === 'dns_change') {
      const runningDns = dnsChanges.find(d => d.status === 'running');
      if (runningDns) {
        setDNSChanges((prev) =>
          prev.map((d) => (d.id === runningDns.id ? { ...d, status: 'failed' } : d))
        );
      }
    }
  };

  const handleRollback = (event: CutoverEvent) => {
    addLog(`Rollback: ${event.message}`);
  };

  const handleCutoverComplete = (event: CutoverEvent) => {
    addLog(`Cutover ${event.status === 'completed' ? 'completed successfully' : 'rolled back'}`);
    setIsCuttingOver(false);
    if (event.status === 'completed') {
      setCutoverComplete(true);
      setCurrentPhase('done');
    }
  };

  // Start cutover
  const handleStartCutover = async () => {
    setError(null);
    setCutoverError(null);

    const { changes, checks } = dnsChanges.length > 0
      ? { changes: dnsChanges, checks: healthChecks }
      : buildFromManifest();

    if (changes.length === 0) {
      // No DNS changes, skip cutover
      setCutoverComplete(true);
      return;
    }

    try {
      setIsCuttingOver(true);
      setCutoverComplete(false);
      setLogs([]);
      setCurrentPhase('pre_check');

      // Reset statuses
      setDNSChanges(changes.map(c => ({ ...c, status: 'pending' })));
      setHealthChecks(checks.map(c => ({ ...c, status: 'pending', error: undefined })));

      addLog(`Starting cutover${isDryRun ? ' (dry run)' : ''}...`);

      // Build request
      const preChecks: HealthCheckRequest[] = checks
        .filter((c) => c.type === 'pre')
        .map((c) => ({
          id: c.id,
          name: c.name,
          type: 'http' as const,
          endpoint: c.endpoint,
          timeout_seconds: 30,
        }));

      const postChecks: HealthCheckRequest[] = checks
        .filter((c) => c.type === 'post')
        .map((c) => ({
          id: c.id,
          name: c.name,
          type: 'http' as const,
          endpoint: c.endpoint,
          timeout_seconds: 30,
        }));

      const dnsChangeRequests: DNSChangeRequest[] = changes.map((c) => ({
        id: c.id,
        domain: c.domain,
        record_type: c.recordType as 'A' | 'CNAME' | 'AAAA',
        old_value: c.oldValue,
        new_value: c.newValue,
        ttl: 300,
      }));

      const request: CreateCutoverRequest = {
        bundle_id: bundleId || '',
        name: 'Migration Cutover',
        pre_checks: preChecks,
        dns_changes: dnsChangeRequests,
        post_checks: postChecks,
        dry_run: isDryRun,
      };

      // Start cutover
      const response = await startCutover(request);
      setCutoverId(response.cutover_id);

      addLog(`Cutover started with ID: ${response.cutover_id}`);

      // Subscribe to SSE for real-time updates
      unsubscribeRef.current = subscribeToCutover(response.cutover_id, {
        onStepStart: handleStepStart,
        onStepComplete: handleStepComplete,
        onStepFailed: handleStepFailed,
        onRollback: handleRollback,
        onComplete: handleCutoverComplete,
        onError: (event) => {
          setCutoverError(event.error || 'Cutover failed');
          addLog(`Error: ${event.error}`);
        },
        onClose: () => {
          if (!cutoverComplete && isCuttingOver) {
            addLog('Connection closed');
          }
        },
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to start cutover';
      setError(message);
      setCutoverError(message);
      setIsCuttingOver(false);
      addLog(`Error: ${message}`);
    }
  };

  // Cancel cutover
  const handleCancel = async () => {
    if (cutoverId) {
      try {
        await cancelCutover(cutoverId);
        addLog('Cutover cancelled');
      } catch {
        addLog('Failed to cancel cutover');
      }
    }
    if (unsubscribeRef.current) {
      unsubscribeRef.current();
    }
    setIsCuttingOver(false);
  };

  // Manual rollback
  const handleRollbackClick = async () => {
    if (cutoverId) {
      try {
        await rollbackCutover(cutoverId);
        addLog('Rollback initiated');
      } catch {
        addLog('Failed to initiate rollback');
      }
    }
  };

  // Reset
  const handleReset = () => {
    if (unsubscribeRef.current) {
      unsubscribeRef.current();
    }
    setCutoverComplete(false);
    setCutoverError(null);
    setCurrentPhase('pre_check');
    setLogs([]);
    setCutoverId(null);
    const { changes, checks } = buildFromManifest();
    setDNSChanges(changes);
    setHealthChecks(checks);
  };

  // Calculate progress
  const preChecks = healthChecks.filter((c) => c.type === 'pre');
  const postChecks = healthChecks.filter((c) => c.type === 'post');
  const allPreChecksPassed = preChecks.every((c) => c.status === 'passed');
  const allDNSCompleted = dnsChanges.every((d) => d.status === 'completed');
  const allPostChecksPassed = postChecks.every((c) => c.status === 'passed');

  const hasDataToCutover = dnsChanges.length > 0;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h3 className="text-lg font-semibold mb-2">DNS Cutover</h3>
        <p className="text-muted-foreground">
          {hasDataToCutover
            ? 'Switch traffic from your cloud infrastructure to the self-hosted Docker containers.'
            : 'No DNS changes required for this migration.'}
        </p>
      </div>

      {/* Dry run toggle */}
      {!isCuttingOver && !cutoverComplete && hasDataToCutover && (
        <div className="bg-warning/10 border border-warning/30 rounded-lg p-4">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-3">
              <Shield className="w-5 h-5 text-warning" />
              <div>
                <p className="font-medium">Dry Run Mode</p>
                <p className="text-sm text-muted-foreground">
                  Simulate the cutover without making actual DNS changes
                </p>
              </div>
            </div>
            <label className="flex items-center gap-2 cursor-pointer">
              <input
                type="checkbox"
                checked={isDryRun}
                onChange={(e) => setIsDryRun(e.target.checked)}
                className="w-4 h-4 rounded border-border"
              />
              <span className="text-sm font-medium">
                {isDryRun ? 'Enabled' : 'Disabled'}
              </span>
            </label>
          </div>
        </div>
      )}

      {/* No cutover needed */}
      {!hasDataToCutover && !cutoverComplete && (
        <div className="bg-info/5 border border-info/20 rounded-lg p-4">
          <div className="flex items-start gap-3">
            <AlertCircle className="w-5 h-5 text-info flex-shrink-0" />
            <div>
              <p className="font-medium text-info">No DNS Cutover Required</p>
              <p className="text-sm text-muted-foreground mt-1">
                Your migration doesn't include any DNS changes. You can complete the wizard.
              </p>
            </div>
          </div>
        </div>
      )}

      {/* Pre-checks */}
      {hasDataToCutover && preChecks.length > 0 && (
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <div
              className={cn(
                'w-6 h-6 rounded-full flex items-center justify-center text-xs font-bold',
                currentPhase === 'pre_check' && 'bg-primary text-primary-foreground',
                allPreChecksPassed && 'bg-accent text-accent-foreground',
                currentPhase !== 'pre_check' && !allPreChecksPassed && 'bg-muted text-muted-foreground'
              )}
            >
              1
            </div>
            <h4 className="font-medium">Pre-Cutover Checks</h4>
            {allPreChecksPassed && <CheckCircle2 className="w-4 h-4 text-accent" />}
          </div>
          <div className="ml-8 space-y-2">
            {preChecks.map((check) => (
              <div
                key={check.id}
                className={cn(
                  'flex items-center justify-between p-3 rounded-lg border',
                  check.status === 'running' && 'border-primary/50 bg-primary/5',
                  check.status === 'passed' && 'border-accent/50 bg-accent/5',
                  check.status === 'failed' && 'border-error/50 bg-error/5'
                )}
              >
                <div className="flex items-center gap-3">
                  {check.status === 'pending' && (
                    <div className="w-4 h-4 rounded-full border-2 border-muted-foreground/30" />
                  )}
                  {check.status === 'running' && (
                    <Loader2 className="w-4 h-4 text-primary animate-spin" />
                  )}
                  {check.status === 'passed' && <CheckCircle2 className="w-4 h-4 text-accent" />}
                  {check.status === 'failed' && <XCircle className="w-4 h-4 text-error" />}
                  <div>
                    <p className="font-medium text-sm">{check.name}</p>
                    <p className="text-xs text-muted-foreground">{check.endpoint}</p>
                  </div>
                </div>
                {check.error && (
                  <span className="text-xs text-error">{check.error}</span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* DNS Changes */}
      {hasDataToCutover && (
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <div
              className={cn(
                'w-6 h-6 rounded-full flex items-center justify-center text-xs font-bold',
                currentPhase === 'dns' && 'bg-primary text-primary-foreground',
                allDNSCompleted && 'bg-accent text-accent-foreground',
                currentPhase !== 'dns' && !allDNSCompleted && 'bg-muted text-muted-foreground'
              )}
            >
              2
            </div>
            <h4 className="font-medium">DNS Changes</h4>
            {allDNSCompleted && <CheckCircle2 className="w-4 h-4 text-accent" />}
          </div>
          <div className="ml-8 space-y-2">
            {dnsChanges.map((change) => (
              <div
                key={change.id}
                className={cn(
                  'p-3 rounded-lg border',
                  change.status === 'running' && 'border-primary/50 bg-primary/5',
                  change.status === 'completed' && 'border-accent/50 bg-accent/5',
                  change.status === 'failed' && 'border-error/50 bg-error/5'
                )}
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-3">
                    <Globe className="w-4 h-4 text-muted-foreground" />
                    <div>
                      <p className="font-medium text-sm">{change.domain}</p>
                      <p className="text-xs text-muted-foreground">
                        {change.recordType} record
                      </p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    {change.status === 'pending' && (
                      <div className="w-4 h-4 rounded-full border-2 border-muted-foreground/30" />
                    )}
                    {change.status === 'running' && (
                      <Loader2 className="w-4 h-4 text-primary animate-spin" />
                    )}
                    {change.status === 'completed' && (
                      <CheckCircle2 className="w-4 h-4 text-accent" />
                    )}
                    {change.status === 'failed' && <XCircle className="w-4 h-4 text-error" />}
                  </div>
                </div>
                <div className="mt-2 flex items-center gap-2 text-xs text-muted-foreground">
                  <code className="bg-muted px-1 rounded">{change.oldValue}</code>
                  <ArrowRight className="w-3 h-3" />
                  <code className="bg-accent/20 px-1 rounded text-accent">{change.newValue}</code>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Post-checks */}
      {hasDataToCutover && postChecks.length > 0 && (
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <div
              className={cn(
                'w-6 h-6 rounded-full flex items-center justify-center text-xs font-bold',
                currentPhase === 'post_check' && 'bg-primary text-primary-foreground',
                allPostChecksPassed && 'bg-accent text-accent-foreground',
                currentPhase !== 'post_check' && !allPostChecksPassed && 'bg-muted text-muted-foreground'
              )}
            >
              3
            </div>
            <h4 className="font-medium">Post-Cutover Validation</h4>
            {allPostChecksPassed && <CheckCircle2 className="w-4 h-4 text-accent" />}
          </div>
          <div className="ml-8 space-y-2">
            {postChecks.map((check) => (
              <div
                key={check.id}
                className={cn(
                  'flex items-center justify-between p-3 rounded-lg border',
                  check.status === 'running' && 'border-primary/50 bg-primary/5',
                  check.status === 'passed' && 'border-accent/50 bg-accent/5',
                  check.status === 'failed' && 'border-error/50 bg-error/5'
                )}
              >
                <div className="flex items-center gap-3">
                  {check.status === 'pending' && (
                    <div className="w-4 h-4 rounded-full border-2 border-muted-foreground/30" />
                  )}
                  {check.status === 'running' && (
                    <Loader2 className="w-4 h-4 text-primary animate-spin" />
                  )}
                  {check.status === 'passed' && <CheckCircle2 className="w-4 h-4 text-accent" />}
                  {check.status === 'failed' && <XCircle className="w-4 h-4 text-error" />}
                  <div>
                    <p className="font-medium text-sm">{check.name}</p>
                    <p className="text-xs text-muted-foreground">{check.endpoint}</p>
                  </div>
                </div>
                {check.error && (
                  <span className="text-xs text-error">{check.error}</span>
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Logs */}
      {logs.length > 0 && (
        <div className="bg-muted/50 rounded-lg p-4">
          <h4 className="font-medium text-sm mb-2">Cutover Log</h4>
          <div className="font-mono text-xs max-h-32 overflow-y-auto space-y-1">
            {logs.map((log, index) => (
              <div key={index} className="text-muted-foreground">
                {log}
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Error display */}
      {cutoverError && (
        <div className="bg-error/10 border border-error/30 rounded-lg p-4">
          <div className="flex items-start gap-3">
            <AlertCircle className="w-5 h-5 text-error flex-shrink-0" />
            <div>
              <p className="font-medium text-error">Cutover Failed</p>
              <p className="text-sm text-muted-foreground mt-1">{cutoverError}</p>
            </div>
          </div>
        </div>
      )}

      {/* Actions */}
      <div className="flex items-center justify-between pt-4 border-t border-border">
        {!isCuttingOver && !cutoverComplete && (
          <>
            <button
              onClick={() => {
                setCutoverComplete(true);
              }}
              className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}
            >
              Skip Cutover
            </button>
            <button
              onClick={handleStartCutover}
              className={cn(buttonVariants({ variant: 'primary' }), 'gap-2')}
            >
              <Play className="w-4 h-4" />
              {hasDataToCutover
                ? isDryRun
                  ? 'Run Dry Run'
                  : 'Start Cutover'
                : 'Complete'}
            </button>
          </>
        )}

        {isCuttingOver && (
          <>
            <button
              onClick={handleCancel}
              className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}
            >
              Cancel
            </button>
            <div className="text-sm text-muted-foreground">
              Cutover in progress...
            </div>
          </>
        )}

        {cutoverComplete && (
          <>
            <div className="flex items-center gap-2">
              <button
                onClick={handleReset}
                className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}
              >
                <RotateCcw className="w-4 h-4" />
                Run Again
              </button>
              {cutoverId && !isDryRun && (
                <button
                  onClick={handleRollbackClick}
                  className={cn(buttonVariants({ variant: 'error' }), 'gap-2')}
                >
                  Rollback
                </button>
              )}
            </div>
            <button
              onClick={nextStep}
              className={buttonVariants({ variant: 'primary' })}
            >
              Complete Migration
            </button>
          </>
        )}
      </div>
    </div>
  );
}
