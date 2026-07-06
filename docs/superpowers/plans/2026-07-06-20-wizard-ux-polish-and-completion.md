# Wizard UX Polish And Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the centralized wizard feel like one clear product journey with an explicit review/completion state and no dead-end final button.

**Architecture:** Add a lightweight final `done` step to the wizard state, render a completion screen after cutover, and add a compact journey summary inside the wizard shell. Keep visual changes restrained and consistent with the existing dashboard design.

**Tech Stack:** React, Zustand, lucide-react, Playwright.

---

## Files

- Modify: `web/src/stores/wizard.ts`
- Modify: `web/src/pages/Migrate.tsx`
- Modify: `web/src/components/MigrationWizard/WizardContainer.tsx`
- Create: `web/src/components/MigrationWizard/WizardSummary.tsx`
- Create: `web/src/components/MigrationWizard/steps/CompletionStep.tsx`
- Modify: `web/src/components/MigrationWizard/steps/CutoverStep.tsx`
- Test: `web/tests/a-z-wizard-smoke.spec.ts`

## Task 1: Add failing E2E for final completion state

- [ ] Add this test to `web/tests/a-z-wizard-smoke.spec.ts`:

```ts
test('wizard shows a final completion screen instead of looping on cutover', async ({ page }) => {
  await page.route('**/api/v1/**', async (route) => {
    const url = route.request().url();
    if (url.includes('/wizard/sessions')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({
          id: 'session-1',
          current_step: 'cutover',
          completed_steps: ['analyze', 'export', 'secrets', 'deploy', 'sync'],
          bundle_id: 'bundle-1',
          secrets_resolved: true,
        }),
      });
      return;
    }
    if (url.includes('/cutover/preview')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ pre_checks: [], dns_changes: [], post_checks: [], warnings: [] }),
      });
      return;
    }
    if (url.includes('/runbooks/bundle-1')) {
      await route.fulfill({
        contentType: 'application/json',
        body: JSON.stringify({ id: 'bundle-1', steps: [] }),
      });
      return;
    }
    await route.fulfill({ contentType: 'application/json', body: '{}' });
  });

  await page.goto('/migrate');
  await page.getByRole('button', { name: /Analyze Source/i }).click();
  await page.getByRole('button', { name: /Skip Cutover/i }).click();
  await page.getByRole('button', { name: /Complete Migration/i }).click();
  await expect(page.getByRole('heading', { name: /Migration Complete/i })).toBeVisible();
});
```

- [ ] Run:

```bash
cd web
PATH=/opt/homebrew/bin:$PATH npm run test:e2e -- --reporter=line a-z-wizard-smoke.spec.ts
```

Expected: fails because `nextStep()` on cutover does not render a completion screen.

## Task 2: Add the `done` wizard step

- [ ] Modify `web/src/stores/wizard.ts`:
  - Add `'done'` to `WizardStep`.
  - Add `'done'` to `WIZARD_STEPS`.
  - Add `done: 'Done'` to `STEP_LABELS`.
  - Add `done: 'Migration complete'` to `STEP_DESCRIPTIONS`.
  - Append `'done'` to `SOURCE_WIZARD_STEPS` and `BUNDLE_WIZARD_STEPS`.

- [ ] Update `hydrateFromSession` to keep backend `done` as frontend `done`:

```ts
const toWizardStep = (step: WizardSessionStep): WizardStep => step as WizardStep;
```

- [ ] Run:

```bash
cd web
PATH=/opt/homebrew/bin:$PATH npm run build
```

Expected: TypeScript points to any switch statements that need a `done` case.

## Task 3: Add completion step

- [ ] Create `web/src/components/MigrationWizard/steps/CompletionStep.tsx`:

```tsx
import { CheckCircle2, Home, RotateCcw } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { buttonVariants } from '@/lib/button-variants';
import { cn } from '@/lib/utils';
import { useWizardStore } from '@/stores/wizard';

export function CompletionStep() {
  const navigate = useNavigate();
  const { reset } = useWizardStore();

  const startNew = () => {
    reset();
    navigate('/migrate');
  };

  return (
    <div className="mx-auto max-w-3xl py-10 text-center space-y-6">
      <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-accent/10">
        <CheckCircle2 className="h-8 w-8 text-accent" />
      </div>
      <div>
        <h2 className="text-2xl font-bold">Migration Complete</h2>
        <p className="mt-2 text-muted-foreground">
          The migration journey has completed. Use the operational pages for day-two checks, logs, metrics, and stack management.
        </p>
      </div>
      <div className="flex flex-wrap justify-center gap-3">
        <button onClick={() => navigate('/')} className={cn(buttonVariants({ variant: 'primary' }), 'gap-2')}>
          <Home className="h-4 w-4" />
          Dashboard
        </button>
        <button onClick={startNew} className={cn(buttonVariants({ variant: 'outline' }), 'gap-2')}>
          <RotateCcw className="h-4 w-4" />
          New Migration
        </button>
      </div>
    </div>
  );
}
```

## Task 4: Render completion and persist `done`

- [ ] Modify `web/src/pages/Migrate.tsx`:
  - Import:

```ts
import { CompletionStep } from '@/components/MigrationWizard/steps/CompletionStep';
```

  - Change session patch mapping:

```ts
const current_step = (currentStep === 'upload' ? 'secrets' : currentStep) as WizardSessionStep;
```

must continue to allow `done`.

  - Add render switch case:

```tsx
case 'done':
  return <CompletionStep />;
```

  - Add `done` to `stepsWithOwnNavigation`:

```ts
const stepsWithOwnNavigation = ['analyze', 'export', 'secrets', 'deploy', 'sync', 'cutover', 'done'];
```

- [ ] Modify `web/src/components/MigrationWizard/steps/CutoverStep.tsx` so the final button still calls `nextStep`.

## Task 5: Add a compact journey summary

- [ ] Create `web/src/components/MigrationWizard/WizardSummary.tsx`:

```tsx
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
    <div className="grid gap-2 text-sm md:grid-cols-3 lg:grid-cols-6">
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
```

- [ ] Modify `web/src/components/MigrationWizard/WizardContainer.tsx`:
  - Import `WizardSummary`.
  - Render it under `WizardProgress`:

```tsx
<WizardSummary steps={steps} currentStep={currentStep} completedSteps={completedSteps} />
```

  - Keep the existing `WizardProgress` for clickable progression.

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
git add web/src/stores/wizard.ts web/src/pages/Migrate.tsx web/src/components/MigrationWizard/WizardContainer.tsx web/src/components/MigrationWizard/WizardSummary.tsx web/src/components/MigrationWizard/steps/CompletionStep.tsx web/src/components/MigrationWizard/steps/CutoverStep.tsx web/tests/a-z-wizard-smoke.spec.ts
git commit -m "feat: add polished wizard completion flow"
```
