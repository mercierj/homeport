# Homeport - Project Instructions

## Project Overview
Cloud migration platform helping companies escape AWS/GCP/Azure vendor lock-in and migrate to self-hosted Docker infrastructure.

## Centralized Class Mappings (IMPORT FROM HERE)

All reusable class mappings are in `web/src/lib/diagram-types.ts`:

```tsx
import {
  categoryBadgeClasses,   // compute, storage, database, networking, security, messaging
  categoryIconClasses,    // resource-icon-* wrappers
  statusBadgeClasses,     // running, stopped, error, pending, etc.
  providerBadgeClasses,   // aws, gcp, azure, self-hosted
  categoryColors          // Hex colors for charts
} from '@/lib/diagram-types';
```

**DO NOT create inline color mappings** - always import from diagram-types.ts

## Style Reuse Policy
Before creating new styles, search existing components and stylesheets (CSS, LESS, etc.) for similar patterns. Reuse existing classes to maintain visual coherency.

## Design System - ALWAYS USE THESE CLASSES

### Colors (CSS Variables)
```
bg-primary          - Deep ocean blue (trust, stability)
bg-accent           - Freedom green (digital sovereignty)
bg-success/warning/error/info - Semantic colors
bg-cloud-aws        - AWS orange (source state)
bg-cloud-gcp        - GCP blue (source state)
bg-cloud-azure      - Azure blue (source state)
bg-freedom          - Self-hosted green (target state)
text-muted-foreground - Secondary text
```

### Cards
```tsx
card-stat           - Stat/metric cards (Dashboard)
  card-stat-label   - Label (uppercase, muted)
  card-stat-value   - Large number value
  card-stat-change-positive/negative - Trend indicator

card-resource       - Cloud resource cards (hoverable)
card-resource-selected - Selected state with ring

card-action         - Clickable action cards
card-action-active  - Active state
```

### Badges
```tsx
badge-success/warning/error/info - Status badges
badge-aws/gcp/azure/freedom      - Cloud provider badges
badge-outline                     - Outlined variant
```

### Status Indicators
```tsx
status-dot-success/warning/error/info/muted - Small dots
```

### Forms
```tsx
input / input-error - Text inputs
textarea            - Multi-line input
select              - Dropdown select
label / label-required - Form labels
```

### Tables
```tsx
table-wrapper       - Overflow container
table / table-header / table-body
table-header-row / table-header-cell
table-row / table-cell
```

### Migration Flow (Hero Feature)
```tsx
migration-flow      - Container for source->target
migration-source    - AWS/GCP/Azure source box
migration-arrow     - Arrow between
migration-target    - Docker/self-hosted target box
```

### Resource Icons
```tsx
resource-icon-compute   - Blue (Server)
resource-icon-database  - Purple (Database)
resource-icon-storage   - Green (HardDrive)
resource-icon-network   - Orange (Globe)
resource-icon-security  - Red (Shield)
resource-icon-messaging - Pink (MessageSquare)
```

### Code Blocks
```tsx
code-inline         - Inline code
code-block          - Multi-line code block
terminal / terminal-header / terminal-body - Terminal UI
```

### Loading States
```tsx
skeleton            - Skeleton placeholder (h-4 w-full)
empty-state         - Empty state container
empty-state-icon/title/description
```

### Alerts
```tsx
alert-success/warning/error/info - Full-width alerts
```

### Progress
```tsx
progress            - Progress bar container
progress-indicator  - Progress fill (use style={{width}})
```

### Utilities
```tsx
animate-in          - Fade in animation
glass-card          - Frosted glass effect
gradient-primary    - Blue->green gradient
gradient-freedom    - Freedom green gradient
shadow-glow-primary/success - Glowing shadows
focus-ring          - Standard focus ring
```

### Buttons (via button-variants.ts)
```tsx
variant: primary, secondary, outline, ghost, error
         success, warning, info
         aws, gcp, azure, freedom
size: sm, default, lg, icon
```

## Quick Patterns

### Dashboard Stats Grid
```tsx
<div className="grid gap-4 md:grid-cols-3">
  <div className="card-stat">
    <p className="card-stat-label">Label</p>
    <p className="card-stat-value">42</p>
  </div>
</div>
```

### Resource Card
```tsx
<div className="card-resource">
  <div className="flex items-center gap-3">
    <div className="resource-icon-compute"><Server /></div>
    <div className="flex-1">
      <h4 className="font-medium">Name</h4>
      <p className="text-sm text-muted-foreground">Type</p>
    </div>
    <span className="badge-aws">AWS</span>
  </div>
</div>
```

### Migration Flow
```tsx
<div className="migration-flow">
  <div className="migration-source">AWS EC2</div>
  <ArrowRight className="migration-arrow" />
  <div className="migration-target">Docker</div>
</div>
```

## Full Documentation
- `DESIGN_CHEATSHEET.md` - Quick reference (recommended)
- `DESIGN_SYSTEM.md` - Complete component library
- `DESIGN_TOKENS.md` - All CSS variable values
- `web/src/index.css` - CSS implementation

## Tech Stack
- Go 1.24 (backend CLI + API)
- React 19 + TypeScript (frontend)
- Tailwind CSS with CSS variables
- Tanstack Query for data fetching
- Lucide icons

## Key Directories
- `internal/cli/` - CLI commands
- `internal/api/` - REST API handlers
- `internal/infrastructure/mapper/` - Cloud service mappers
- `internal/infrastructure/parser/` - Cloud config parsers
- `web/src/pages/` - React pages
- `web/src/components/` - React components

## Sprint Execution
- When executing a plan divided into sprints, ask the user if you should continue before starting the next sprint when context is running low
