# Centralized A To Z UX Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the existing A-to-Z migration wizard into the single clear product path for migration, export, deployment, sync, and cutover.

**Architecture:** Keep `/migrate` as the canonical journey. Remove competing primary `/deploy` UX, move any useful deployment capabilities into the wizard deploy step, add a final completion surface, and verify with E2E checks that users cannot find multiple equivalent paths.

**Tech Stack:** React, Zustand, React Router, Playwright, Go API acceptance tests.

---

## `/goal` Handoff

Use this as the next agent objective:

```text
/goal Execute the remaining centralized HomePort A-to-Z UX plans in order:
docs/superpowers/plans/2026-07-06-18-route-and-entry-consolidation.md,
docs/superpowers/plans/2026-07-06-19-unified-wizard-deploy-step.md,
docs/superpowers/plans/2026-07-06-20-wizard-ux-polish-and-completion.md,
docs/superpowers/plans/2026-07-06-21-centralized-ux-acceptance.md.
After each plan, run its verification commands, commit, then continue. Preserve unrelated dirty files. Do not claim "single clear A-to-Z UX" until Plan 21 passes.

Context:
- Current `/migrate` is the canonical A-to-Z wizard.
- Current `/deploy` is still a competing primary deployment path and must stop being presented as an equal path.
- Keep advanced operational pages like Stacks, Terminal, Logs, Metrics; only remove duplicated migration/deploy journey entry points.
- If a plan conflicts with current code, implement the plan intent with the smallest working diff.
```

## Execution Order

1. `2026-07-06-18-route-and-entry-consolidation.md`
2. `2026-07-06-19-unified-wizard-deploy-step.md`
3. `2026-07-06-20-wizard-ux-polish-and-completion.md`
4. `2026-07-06-21-centralized-ux-acceptance.md`

## Done Means

- Sidebar has one migration/deployment journey entry: `Migrate`.
- `/deploy` no longer presents a separate deploy wizard; it redirects to `/migrate`.
- Dashboard pending deployment CTA opens `/migrate`, not `/deploy`.
- Wizard deploy step contains local, SSH, cloud provider comparison/config, Terraform ZIP export, plan, and apply.
- Wizard has one final completion state after cutover, with restart and dashboard actions.
- E2E tests prove there is no visible duplicate deploy path and that the source and bundle entries remain usable.
