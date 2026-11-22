---
name: rn-foundation-quality-review
description: >-
  [DRAFT][Stage 3][RN] Review observed React Native UI behavior when asked for
  runtime QA, interaction validation, accessibility, keyboard, safe-area, sheet,
  back, focus, localization, or adaptive-layout acceptance. Use device,
  simulator, or equivalent runtime evidence. Do not use for ordinary
  implementation, static architecture, or visual preference review.
metadata:
  status: draft
  version: "1.0"
  stages: [stage-3-a, stage-3-b]
  scopes: [stack:rn]
  roles: [reviewer-a, reviewer-b]
---

# RN Foundation Quality Review

Read `docs/rn-foundation-ui/README.md`, the product requirement, and current source for
the surface under review. Test only scenarios relevant to the change or acceptance request.

## Review

- Record the platform, OS/runtime, build, route, data state, and exact action.
- Observe the relevant keyboard, safe-area, back/dismiss, focus, accessibility, long-copy,
  loading/error, or constrained-window behavior.
- Compare with the product requirement and platform expectation, not a generic visual style.
- Keep unsupported platforms and scenarios outside the result unless the user asks for them.

Do not require the full matrix for every change. A color or spacing change does not need
sheet and keyboard acceptance. A changed formSheet or navigation path does.

## Evidence boundary

Do not use source inspection, typecheck, tests, checker output, or a static screenshot as
runtime PASS evidence for keyboard, gesture, focus restoration, dismissal, screen-reader
order, or security timing. Mark unobserved cases `OPEN / UNVERIFIED`; do not report them as
observed failures.

## Report

Return:

```text
Result: RUNTIME ACCEPTED | NEEDS CORRECTION | OPEN / UNVERIFIED
Environment/evidence:
Findings:
Open cases:
Recommended acceptance checks:
```

Record the platform, OS/runtime, and exact scenario for every PASS or FAIL. Do
not carry a result to a materially changed owner or presentation path without
re-observing it.

Do not turn visual preference into a hard failure without a product reference. Modify
code only when the user asks for a fix.
