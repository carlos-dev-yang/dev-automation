---
name: rn-foundation-standard
description: >-
  [DRAFT][Stage 2][RN] Build or change React Native product UI while applying
  the repository's minimal native-ownership boundary and preserving app-owned
  visual freedom. Use for screens, forms, lists, tabs, modal or sheet
  presentation, responsive layout, theme work, or migration away from a shared
  visual package.
metadata:
  status: draft
  version: "1.0"
  stages: [stage-2]
  scopes: [stack:rn]
  roles: [implementer]
---

# RN Foundation Standard

Read `docs/rn-foundation-ui/README.md` before changing product UI. Read the product
requirement, current source, and app `docs/UI_STATUS.md` only when they are relevant.

## Work

1. Record the product behavior that must survive, including route results, state,
   persistence, accessibility meaning, disabled/loading behavior, and required IDs.
2. Identify only the native owners affected by the change. Do not audit unrelated
   keyboard, navigation, or accessibility paths for a visual-only edit.
3. Prefer the installed RN, Expo Router, React Navigation, and platform API when it
   already owns the behavior.
4. Keep visual composition feature-local unless the same product meaning is already
   repeated across independent features. Let the app choose all visual details.
5. Implement a coherent slice, then run targeted static checks. Observe runtime only
   for native-sensitive behavior that actually changed.

Treat `@app-automation/rn-foundation` as a headless theme/token contract. Do not add
visual components or platform runtime behavior to it.

## Preserve freedom

- Do not require a shared component, folder template, visual recipe, or app-independent
  card/button style.
- Do not reject feature-local UI merely because it is not generalized.
- Do not preserve a wrapper, provider, adapter, recipe, or checker rule when it duplicates
  or blocks the current native owner.
- Do not hard-code keyboard, safe-area, tab-bar, sheet, or device geometry.
- Do not add test code unless the user asks for it.

## Report

Report preserved behavior, changed owners, targeted checks, and runtime observations
separately. Mark unobserved native-sensitive results `OPEN / UNVERIFIED` without treating
them as observed failures.
