---
name: ops-remediation-director
description: >-
  [Stage 4][Fan-in Director] 병렬 실행된 골든셋 A와 골든셋 B 검토가 모두 끝난 뒤,
  두 리포트와 실제 작업 diff·경로를 통합 분석해 근본 원인을 진단하고 단일 종합 교정
  지시서를 작성한다. 스킬·청사진 결함이 있으면 반드시 '스킬 교정 선행 → 작업 교정 후행'
  순서를 강제한다. A 또는 B 중 하나만 끝난 상태에서는 실행하지 않는다.
metadata:
  status: official
  version: "1.0"
  stages: [stage-4]
  scopes: [core]
  roles: [director]
---

# Remediation Director: 통합 근본 원인 진단 & 선 스킬 → 후 작업 교정 지시

골든셋 실패 시 구현 AI에게 실패 로그만 던져 땜질(Reward Hacking)하게 두지 않고, **시니어 리드 역할을 대리해 A·B 결과를 한 번에 묶어 근본 교정 순서와 방향을 지시**한다.

## 진입 선행 조건 (Fan-in Barrier)

- **골든셋 A(`eval-release-pr-gate`)와 골든셋 B(`eval-senior-quality-gate`)의 검토가 모두 완료**되었는지 확인한다.
- 한쪽 검토만 끝난 상태라면 즉시 중단하고, 나머지 검토 완료를 기다린다(핑퐁 방지).

## 실행 절차

1. **A + B 리포트 및 작업 경로 통합 분석**
   - 골든셋 A의 실패 항목, 골든셋 B의 `CRITICAL`(및 관련 `MEDIUM`) 항목, 실제 변경 diff, 작업 시 적용했던 스킬을 한 테이블에 대조한다.
   - 서로 연결된 표면 증상들을 **공통 근본 원인(Root Cause)** 단위로 묶는다.
2. **원인 2분류**
   - **Type S (스킬·청사진 결함)**: 스킬에 금지/구현 규칙이 없거나 모호했거나, Stage 1 청사진에서 누락되어 구현 AI가 잘못된 방향으로 간 경우.
   - **Type C (순수 코드 실수)**: 기존 스킬과 청사진에 이미 명확히 적혀 있었으나 구현 AI가 빠뜨리거나 잘못 구현한 경우.
3. **엄격한 교정 순서 결정 — 스킬 교정 선행 → 작업 교정 후행**
   - **Type S가 1건이라도 존재하면**:
     - **Step 1 (스킬 교정 선행)**: 작업 코드를 먼저 고치지 않는다. 중앙 `rd-skills` 레포의 [SKILL_AUTHORING_GUIDE.md](../../SKILL_AUTHORING_GUIDE.md)에 따라 신규 실험 스킬(`metadata.status: draft`) 또는 기존 스킬 클린 재작성(Rewrite) PR 초안을 먼저 작성해 사람 승인을 요청한다.
     - **Step 2 (작업 교정 후행)**: 교정된 스킬이 반영된 뒤, 구현 AI(`AI #1`)가 최신 스킬과 아래 종합 수정 지시서를 기준으로 코드를 근본 교정하게 한다.
   - **Type C만 존재하면**:
     - 스킬 변경 없이 바로 구현 AI(`AI #1`)용 종합 수정 지시서를 전달한다.
4. **구현 AI용 종합 수정 지시서 작성 (Reward Hacking 차단)**
   - 각 수정 항목에 **참조해야 할 정확한 스킬과 원칙**, **구조적 수정 방향**, **절대 금지할 땜질 방식(예: 하드코딩·테스트 우회·예외 삼킴)**을 명시한다.

## 출력 형식

```text
Remediation Sequence:
[SKILL CORRECTION FIRST -> THEN CODE CORRECTION] | [CODE CORRECTION ONLY]

1. Unified Root-Cause Analysis (Golden Set A + B):
   - RC-1 [Type S | Type C]: 근본 원인 요약
     - Linked Findings: [A-#, B-#]
     - Evidence: [파일/diff 경로]

2. Step 1 — Skill / Blueprint Correction Plan (Type S 존재 시 필수 선행):
   - Target Repo: rd-skills (또는 Stage 1 청사진)
   - Action: [신규 status:draft 추가 | 기존 스킬 클린 재작성 PR]
   - Target Skill & Change Summary: [기존 규칙 충돌 점검 포함]
   - Gate: 사람 검토·승인 후 Step 2 진행

3. Step 2 — Unified Code Correction Directive (for AI #1 Implementer):
   - Required Skills to Load First: [교정 완료된 스킬 포함 목록]
   - Mandatory Fixes:
     1) [대상 모듈/파일] 근본 수정 방향 + 기대 동작
   - Anti-Hacking Constraints (금지사항):
     - [테스트 통과용 하드코딩/우회 금지 항목]
   - Re-Verification: 수정 완료 후 골든셋 A·B 병렬 재실행
```
