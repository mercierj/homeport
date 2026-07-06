# Unified Wizard Deploy Step Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the remaining useful `/deploy` capabilities into the A-to-Z wizard deploy step so local, SSH, cloud Terraform ZIP, Terraform plan, and Terraform apply live in one place.

**Architecture:** Reuse existing deployment API functions and existing provider components. Extend the wizard deploy step with a compact target selector and cloud subflow; do not create another global store or second deploy wizard.

**Tech Stack:** React, Zustand wizard store, existing `deploy-api`, existing `migrate-api`, existing `DeploymentWizard` provider components.

---

## Files

- Modify: `web/src/stores/wizard.ts`
- Modify: `web/src/pages/Migrate.tsx`
- Modify: `web/src/components/MigrationWizard/steps/DeployStep.tsx`
- Modify: `web/src/pages/Deploy.tsx`
- Test: `web/tests/a-z-wizard-smoke.spec.ts`

## Task 1: Add failing E2E for unified deploy choices inside `/migrate`

- [ ] Extend `web/tests/a-z-wizard-smoke.spec.ts` with:

```ts
test('wizard deploy step exposes local ssh and cloud choices in one flow', async ({ page }) => {
  await page.route('**/api/v1/**', async (route) => {
    const url = route.request().url();
    if (url.includes('/wizard/sessions')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'session-1',
          current_step: 'deploy',
          completed_steps: ['analyze', 'export', 'secrets'],
          bundle_id: 'bundle-1',
          secrets_resolved: true,
        }),
      });
      return;
    }
    if (url.includes('/bundle/bundle-1/compose')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ content: 'services:\\n  app:\\n    image: nginx' }),
      });
      return;
    }
    await route.fulfill({ contentType: 'application/json', body: '{}' });
  });

  await page.goto('/migrate');
  await page.getByRole('button', { name: /Analyze Source/i }).click();
  await expect(page.getByText('Select Deployment Target')).toBeVisible();
  await expect(page.getByRole('button', { name: /Local Docker/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /Remote SSH/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /Cloud Provider/i })).toBeVisible();
  await expect(page.getByRole('button', { name: /Download Docker ZIP/i })).toBeVisible();
});
```

- [ ] Run:

```bash
cd web
PATH=/opt/homebrew/bin:$PATH npm run test:e2e -- --reporter=line a-z-wizard-smoke.spec.ts
```

Expected: fails because the wizard deploy step currently only exposes local and SSH.

## Task 2: Hydrate wizard state from the created session

- [ ] Modify `web/src/pages/Migrate.tsx`:
  - Destructure `hydrateFromSession` from `useWizardStore()`.
  - Replace the create-session success path:

```ts
.then((session) => setSessionId(session.id))
```

with:

```ts
.then((session) => {
  const isAdvancedSession =
    session.current_step !== 'analyze' ||
    session.completed_steps.length > 0 ||
    !!session.bundle_id ||
    session.secrets_resolved;

  if (isAdvancedSession) {
    hydrateFromSession(session);
    return;
  }

  setSessionId(session.id);
})
```

  - Add `hydrateFromSession` to the `useEffect` dependency list.
  - Keep `setSessionId(session.id)` for fresh sessions so choosing `Upload Bundle` does not get overwritten back to `Analyze`.

- [ ] Run:

```bash
cd web
PATH=/opt/homebrew/bin:$PATH npm run test:e2e -- --reporter=line a-z-wizard-smoke.spec.ts
```

Expected: existing bundle-entry smoke still passes; the new deploy test may still fail until the next tasks add cloud controls.

## Task 3: Extend wizard deploy target state

- [ ] Modify `web/src/stores/wizard.ts`. Replace:

```ts
export type DeployTarget = 'local' | 'ssh';
```

with:

```ts
export type DeployTarget = 'local' | 'ssh' | 'cloud';
```

- [ ] Run:

```bash
cd web
PATH=/opt/homebrew/bin:$PATH npm run build
```

Expected: build passes or points to switch/case sites that need cloud handling in Task 3.

## Task 4: Add cloud/manual controls to `DeployStep`

- [ ] Modify imports in `web/src/components/MigrationWizard/steps/DeployStep.tsx`:

```ts
import { Cloud, Download, Server, Globe, Key, Terminal, CheckCircle2, XCircle, Loader2, Play, RotateCcw, AlertCircle } from 'lucide-react';
import { ProviderComparison } from '@/components/DeploymentWizard/ProviderComparison';
import { ProviderConfigForm } from '@/components/DeploymentWizard/ProviderConfigForm';
import { TerraformExport } from '@/components/DeploymentWizard/TerraformExport';
import { applyCloudDeploy, getCloudDeploy, startCloudDeploy, startDeployment, subscribeToDeployment, cancelDeployment, type CloudDeployJob, type LocalDeployConfig, type SSHDeployConfig, type PhaseEvent, type LogEvent, type ErrorEvent } from '@/lib/deploy-api';
import { downloadStack } from '@/lib/migrate-api';
import { useDeploymentStore } from '@/stores/deployment';
import type { Provider } from '@/lib/providers-api';
```

- [ ] Inside `DeployStep`, add cloud state near the existing `useState` calls:

```ts
const [cloudStep, setCloudStep] = useState<'compare' | 'configure' | 'export'>('compare');
const [selectedCloudProvider, setSelectedCloudProvider] = useState<'hetzner' | 'scaleway' | 'ovh' | null>(null);
const [selectedCloudBaseCost, setSelectedCloudBaseCost] = useState(0);
const [cloudJob, setCloudJob] = useState<CloudDeployJob | null>(null);
const { cloudConfig, setCloudProvider } = useDeploymentStore();
```

- [ ] Include `selectedResources` in the existing `useWizardStore()` destructure.

- [ ] Add these helpers inside `DeployStep`:

```ts
const cloudResources = selectedResources;
const cloudMappingResults = {
  resources: cloudResources,
  warnings: bundleManifest ? [] : [],
  provider: bundleManifest?.source?.provider ?? 'unknown',
};
const isCloudProvider = (provider: Provider): provider is 'hetzner' | 'scaleway' | 'ovh' =>
  provider === 'hetzner' || provider === 'scaleway' || provider === 'ovh';
const cloudProjectName = cloudConfig.domain || 'homeport-cloud';
const pollCloudJob = async (id: string): Promise<CloudDeployJob> => {
  for (;;) {
    const job = await getCloudDeploy(id);
    setCloudJob(job);
    if (job.status === 'planned' || job.status === 'applied' || job.status === 'failed') return job;
    await new Promise((resolve) => setTimeout(resolve, 1000));
  }
};
const handleCloudProviderSelect = (provider: Provider, baseCost: number) => {
  if (!isCloudProvider(provider)) return;
  setCloudProvider(provider);
  setSelectedCloudProvider(provider);
  setSelectedCloudBaseCost(baseCost);
  setCloudJob(null);
  setCloudStep('configure');
};
const handleCloudPlan = async () => {
  if (!selectedCloudProvider || !cloudConfig.region) {
    setError('Please select a cloud provider and region');
    return;
  }
  const job = await startCloudDeploy({
    resources: cloudResources,
    config: {
      provider: selectedCloudProvider,
      project_name: cloudProjectName,
      domain: cloudConfig.domain,
      region: cloudConfig.region.id,
    },
    apply: false,
  });
  setCloudJob(job);
  await pollCloudJob(job.id);
};
const handleCloudApply = async () => {
  if (!cloudJob?.id) return;
  setCloudJob(await applyCloudDeploy(cloudJob.id));
  await pollCloudJob(cloudJob.id);
};
const handleDockerZipDownload = async () => {
  if (selectedResources.length === 0) {
    setError('Select resources before exporting Docker ZIP.');
    return;
  }
  const blob = await downloadStack(selectedResources, {
    domain: bundleManifest?.source?.provider || 'homeport.local',
    consolidate: true,
    include_migration: true,
    include_monitoring: true,
    ha: false,
  });
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = 'homeport-docker-stack.zip';
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
};
```

- [ ] Replace the deployment target grid in `DeployStep` with three buttons:

```tsx
<div className="grid grid-cols-1 md:grid-cols-3 gap-4">
  <button onClick={() => setDeployTarget('local')} className={cn('card-action p-6 text-left', deployTarget === 'local' && 'card-action-active border-primary')}>
    <Server className="w-6 h-6 text-primary mb-3" />
    <h4 className="font-semibold">Local Docker</h4>
    <p className="text-sm text-muted-foreground mt-1">Deploy this bundle on the current machine.</p>
  </button>
  <button onClick={() => setDeployTarget('ssh')} className={cn('card-action p-6 text-left', deployTarget === 'ssh' && 'card-action-active border-primary')}>
    <Globe className="w-6 h-6 text-accent mb-3" />
    <h4 className="font-semibold">Remote SSH</h4>
    <p className="text-sm text-muted-foreground mt-1">Deploy this bundle to a remote Docker host.</p>
  </button>
  <button onClick={() => setDeployTarget('cloud')} className={cn('card-action p-6 text-left', deployTarget === 'cloud' && 'card-action-active border-primary')}>
    <Cloud className="w-6 h-6 text-accent mb-3" />
    <h4 className="font-semibold">Cloud Provider</h4>
    <p className="text-sm text-muted-foreground mt-1">Compare EU providers, export Terraform, plan, then apply.</p>
  </button>
</div>
```

- [ ] Directly under the target grid, add a single manual export button that stays visible before and after selecting a target:

```tsx
<button onClick={handleDockerZipDownload} className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}>
  <Download className="w-4 h-4" />
  Download Docker ZIP
</button>
```

- [ ] Add cloud rendering below SSH config:

```tsx
{deployTarget === 'cloud' && cloudStep === 'compare' && (
  <ProviderComparison
    mappingResults={cloudMappingResults}
    onSelect={handleCloudProviderSelect}
    onBack={() => setDeployTarget(null)}
  />
)}
{deployTarget === 'cloud' && cloudStep === 'configure' && selectedCloudProvider && (
  <>
    <ProviderConfigForm
      provider={selectedCloudProvider}
      baseCost={selectedCloudBaseCost}
      onBack={() => setCloudStep('compare')}
      onDeploy={handleCloudPlan}
      deployLabel={cloudJob?.status === 'running' ? 'Running Terraform...' : 'Plan Terraform Deploy'}
    />
    <div className="flex flex-wrap items-center gap-3 border-t pt-4">
      <button onClick={() => setCloudStep('export')} disabled={!cloudConfig.region} className={buttonVariants({ variant: 'outline' })}>
        Download Terraform ZIP
      </button>
      {cloudJob?.status === 'planned' && (
        <button onClick={handleCloudApply} className={buttonVariants({ variant: 'primary' })}>
          Apply Terraform
        </button>
      )}
      {cloudJob && (
        <span className="text-sm text-muted-foreground">
          Terraform job {cloudJob.status}{cloudJob.error ? `: ${cloudJob.error}` : ''}
        </span>
      )}
    </div>
  </>
)}
{deployTarget === 'cloud' && cloudStep === 'export' && selectedCloudProvider && cloudConfig.region && (
  <TerraformExport
    provider={selectedCloudProvider}
    resources={cloudResources}
    config={{
      project_name: cloudProjectName,
      domain: cloudConfig.domain,
      region: cloudConfig.region.id,
    }}
    onBack={() => setCloudStep('configure')}
  />
)}
```

- [ ] Ensure `handleDeploy` only runs for `local` and `ssh`:

```ts
if (deployTarget === 'cloud') {
  setError('Use the cloud provider controls to plan or apply Terraform.');
  return;
}
```

## Task 5: Retire the duplicate page implementation

- [ ] Replace `web/src/pages/Deploy.tsx` with a redirect-only page:

```tsx
import { Navigate } from 'react-router-dom';

export function Deploy() {
  return <Navigate to="/migrate" replace />;
}
```

- [ ] Run:

```bash
rg -n "TargetSelector|ProviderComparison|TerraformExport|startCloudDeploy" web/src/pages/Deploy.tsx
```

Expected: no matches.

## Task 6: Verify and commit

- [ ] Run:

```bash
git diff --check
cd web && PATH=/opt/homebrew/bin:$PATH npm run build
cd web && PATH=/opt/homebrew/bin:$PATH npm run test:e2e -- --reporter=line a-z-wizard-smoke.spec.ts centralized-entry.spec.ts
```

Expected: build and tests pass.

- [ ] Commit:

```bash
git add web/src/stores/wizard.ts web/src/pages/Migrate.tsx web/src/components/MigrationWizard/steps/DeployStep.tsx web/src/pages/Deploy.tsx web/tests/a-z-wizard-smoke.spec.ts
git commit -m "feat: unify deployment inside migration wizard"
```
