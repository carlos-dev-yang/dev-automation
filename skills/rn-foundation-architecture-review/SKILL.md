---
name: rn-foundation-architecture-review
description: >-
  [DRAFT][Stage 3-B][RN] Review React Native product UI for static native-owner
  conflicts and rn-foundation boundary violations. Use after implementation or
  when asked about navigation, provider, route/feature/shared dependencies,
  duplicated native state, hard-coded geometry, or legacy UI-package ownership.
  Do not use for visual taste, general code-quality, or runtime acceptance.
metadata:
  status: draft
  version: "1.0"
  stages: [stage-3-b]
  scopes: [stack:rn]
  roles: [reviewer-b]
---

# RN Foundation Architecture Review

Read `docs/rn-foundation-ui/README.md`. Review only the requested app or changed
surface and use current source as evidence.

## Check

- Identify an active duplicate owner for navigation, tab selection, back, presentation,
  scroll, keyboard inset, safe area, focus, or gesture.
- Identify dependency direction that transfers route/history policy into a feature or
  product/runtime behavior into `rn-foundation`.
- Identify fixed device geometry or a legacy provider/adapter that still owns behavior
  now handled by the installed native API.
- Verify that error fallback dependencies are available outside the boundary that renders
  them.
- Separate static facts from behavior that needs a device or simulator.

## Do not flag

- app-specific visual choices, feature-local composition, file size, folder naming, or
  missing generalization;
- duplicated-looking JSX with different product meaning;
- an unused prop or listener as an ownership defect unless it is active;
- keyboard, safe-area, gesture, focus, or accessibility results that were not observed;
- broad refactors unrelated to the requested surface.

## Result

For each actionable finding, return severity, exact source evidence, the conflicting
owners, user impact, the smallest correction, behavior to preserve, and remaining runtime.
Classify unobserved native results as `OPEN / UNVERIFIED` rather than failures.

Say `actionable static finding 없음` when no issue meets that boundary. Modify code only
when the user asks for a fix.
