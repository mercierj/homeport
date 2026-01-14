# Design System Migration Plan

## Remaining Files with Inline Colors

### Priority 1: Core UI Components (High Impact)

| File | Patterns | Action |
|------|----------|--------|
| `Terminal.tsx` | `bg-green-500`, `bg-yellow-500`, `bg-red-500` status dots | Add `status-dot-*` classes |
| `QueryEditor.tsx` | `bg-red-50 border-red-200 text-red-700` error | Use `alert-error` |
| `SelectionSummary.tsx` | `bg-gray-100`, `bg-blue-50`, `bg-amber-50` | Use `bg-muted`, `bg-info/10`, `bg-warning/10` |
| `ResourceDetailPanel.tsx` | `bg-gray-100`, `bg-blue-50`, `bg-gray-50` | Use `bg-muted`, `badge-*` |

### Priority 2: Deployment Wizard (Medium Impact)

| File | Patterns | Action |
|------|----------|--------|
| `TargetSelector.tsx` | `border-gray-200 hover:border-blue-500` | Use `card-action` pattern |
| `ConfigurationForm.tsx` | `bg-gray-50`, `bg-amber-50 border-amber-200` | Use `bg-muted`, `alert-warning` |
| `DeploymentExecution.tsx` | `bg-red-500`, `bg-emerald-500`, `bg-gray-200` progress | Add `progress-*` variants |
| `DeploymentLogs.tsx` | `bg-gray-900` terminal | Use `terminal-body` |

### Priority 3: Migration Wizard (7 files)

| File | Common Patterns | Action |
|------|-----------------|--------|
| `ServiceMigrationCard.tsx` | `bg-green-50`, `bg-green-500`, `bg-gray-300` toggle | Add `toggle-*` classes |
| `ServiceCategoryTabs.tsx` | `bg-green-100`, `bg-gray-100`, `bg-blue-50` tabs | Add `tab-*` classes |
| `MigrationConfigStep.tsx` | `bg-gray-50 border-gray-200` | Use `bg-muted` |
| `ComputeMigration.tsx` | `bg-blue-50`, `bg-amber-50`, `bg-gray-50` alerts | Use `alert-info`, `alert-warning` |
| `DatabaseMigration.tsx` | Same patterns | Same actions |
| `NetworkingMigration.tsx` | Same patterns | Same actions |
| `StorageMigration.tsx` | Same patterns | Same actions |
| `MessagingMigration.tsx` | Same patterns | Same actions |
| `SecurityMigration.tsx` | Same patterns | Same actions |

### Priority 4: Credential Inputs (3 files)

| File | Patterns | Action |
|------|----------|--------|
| `DatabaseCredentials.tsx` | `border-gray-300`, `bg-gray-100`, `focus:ring-blue-500` | Use `input` class |
| `CacheCredentials.tsx` | Same patterns | Use `input` class |
| `SecurityCredentials.tsx` | Same patterns | Use `input` class |

### Priority 5: Pages (3 files)

| File | Patterns | Action |
|------|----------|--------|
| `Migrate.tsx` | `bg-gray-50`, `bg-blue-50`, `bg-gray-100` | Use `bg-muted`, semantic colors |
| `LogExplorer.tsx` | Level colors (`bg-red-50`, `bg-yellow-50`, etc.) | Use `log-level-*` classes |
| `MetricsDashboard.tsx` | Severity colors | Use `alert-*` classes |

---

## New CSS Classes Needed

Add to `index.css`:

```css
/* Toggle Switch */
.toggle { @apply relative inline-flex h-6 w-11 items-center rounded-full; }
.toggle-enabled { @apply bg-success; }
.toggle-disabled { @apply bg-muted; }
.toggle-thumb { @apply inline-block h-4 w-4 transform rounded-full bg-white transition; }

/* Tabs */
.tab { @apply px-4 py-2 text-sm font-medium transition-colors; }
.tab-active { @apply bg-primary/10 border-l-4 border-primary text-primary; }
.tab-inactive { @apply border-l-4 border-transparent hover:bg-muted text-muted-foreground; }

/* Log Levels */
.log-level-error { @apply bg-error/10 text-error; }
.log-level-warn { @apply bg-warning/10 text-warning; }
.log-level-info { @apply bg-info/10 text-info; }
.log-level-debug { @apply bg-muted text-muted-foreground; }

/* Progress Variants */
.progress-success { @apply bg-success; }
.progress-error { @apply bg-error; }

/* Chip/Pill for selections */
.chip { @apply px-2 py-0.5 rounded text-xs font-medium bg-muted text-muted-foreground; }
.chip-selected { @apply bg-primary/10 text-primary border border-primary; }
```

---

## Migration Strategy

### Sprint 1: Add new CSS classes (30 min)
1. Add toggle, tab, log-level, progress classes to `index.css`
2. Update `tailwind.config.js` if needed
3. Build and verify

### Sprint 2: Core components (45 min)
1. Update `Terminal.tsx`
2. Update `QueryEditor.tsx`
3. Update `SelectionSummary.tsx`
4. Update `ResourceDetailPanel.tsx`

### Sprint 3: Deployment Wizard (30 min)
1. Update `TargetSelector.tsx`
2. Update `ConfigurationForm.tsx`
3. Update `DeploymentExecution.tsx`
4. Update `DeploymentLogs.tsx`

### Sprint 4: Migration Wizard (1 hour)
1. Update `ServiceMigrationCard.tsx`
2. Update `ServiceCategoryTabs.tsx`
3. Update all 6 `*Migration.tsx` files
4. Update `MigrationConfigStep.tsx`

### Sprint 5: Credential Inputs + Pages (45 min)
1. Update 3 credential input files
2. Update `Migrate.tsx`
3. Update `LogExplorer.tsx`
4. Update `MetricsDashboard.tsx`

### Sprint 6: Cleanup + Documentation (30 min)
1. Final grep check for remaining inline colors
2. Update CLAUDE.md with new classes
3. Update DESIGN_CHEATSHEET.md

---

## Files to Skip (Intentional)

| File | Reason |
|------|--------|
| `DesignShowcase.tsx` | Design system demo page |
| `index.css` | Defines the CSS classes |
| `diagram-types.ts` | Centralized color definitions |
| `*-api.ts` files | Backend helpers (phase 2) |
| `CacheBrowser.tsx` purple/pink | Semantic for data types |

---

## Quick Wins (Bulk Replace)

```bash
# Gray backgrounds
sed -i '' 's/bg-gray-50/bg-muted/g'
sed -i '' 's/bg-gray-100/bg-muted/g'

# Blue info patterns
sed -i '' 's/bg-blue-50 border border-blue-200/alert-info/g'

# Amber warning patterns
sed -i '' 's/bg-amber-50 border border-amber-200/alert-warning/g'

# Red error patterns
sed -i '' 's/bg-red-50 border border-red-200/alert-error/g'
```

---

## Estimated Total Time: ~4 hours

With bulk replacements: ~2.5 hours
