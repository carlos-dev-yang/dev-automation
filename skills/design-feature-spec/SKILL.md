---
name: design-feature-spec
description: >-
  [Stage 1][Architect] 제품 요구사항을 실행 가능한 피처 명세, 상태 머신, 경계값·예외·복구
  흐름, 그리고 골든셋 A용 핵심 수용 테스트 케이스(TC) 초안으로 해체한다. 신규 피처
  기획·동작 스펙 확정 시 사용. 단순 스타일 조정이나 이미 Gate 0을 통과한 코드 구현에는
  사용하지 않는다.
metadata:
  status: official
  version: "1.0"
  stages: [stage-1]
  scopes: [core]
  roles: [architect]
---

# Stage 1: Feature Specification & Acceptance TC Design

모호한 요구사항을 상태 전이와 검증 가능한 계약으로 구체화해, Stage 2 구현 AI가 에지 케이스를 임의로 추측하지 않도록 한다.

## 설계 절차

1. **목표·비목표 및 사용자 시나리오 고정**
   - 핵심 사용자 목표와 명시적 비목표(Non-Goals)를 분리해 범위 팽창을 막는다.
   - Happy Path를 진입 조건(Precondition) → 트리거 → 결과 상태(Postcondition) 순으로 정리한다.
2. **상태 머신 및 불변 조건(Invariants) 정의**
   - 피처가 거치는 모든 상태(예: `idle`, `loading`, `ready`, `submitting`, `error`, `offline`)와 허용 전이를 정의한다.
   - 어떤 상황에서도 깨지면 안 되는 비즈니스 불변 조건(예: 중복 제출 금지, 잔액 음수 불가, 권한 없는 상태 전이 차단)을 명시한다.
3. **예외·경계값·복구 흐름 명시**
   - 네트워크 지연/단절, 동시 요청(경합), 부분 실패, 잘못된 입력, 권한 만료, 앱 중단/재진입 시의 기대 동작과 복구 경로를 빠짐없이 적는다.
   - 롤백 또는 보상 트랜잭션이 필요한 실패 지점을 구분한다.
4. **사전 합의 수용 기준(Pre-Agreed TC) 도출**
   - 구현 전에 골든셋 A(`eval-release-pr-gate`)가 검증할 핵심 비즈니스 TC, 경계값/예외 TC, 핵심 E2E 시나리오를 Given / When / Then 형식으로 고정한다.
   - 사후에 구현에 맞춰 테스트를 끼워 맞추는 편향을 막기 위해 각 TC의 기대 결과와 실패 판정 기준을 구체화한다.
5. **미결 의사결정(Open Decisions) 추출**
   - 제품 정책 판단이 필요한 항목(데이터 삭제 범위, 재시도 정책, 실패 시 사용자 안내 및 차단 여부 등)은 임의로 결정하지 않고 선택지·권장안으로 분리한다.

## 금지사항

- "적절히 에러 처리한다", "자연스럽게 동작한다" 같은 검증 불가능한 모호한 문장으로 스펙을 끝내지 않는다.
- 코드 구현 세부(함수명·내부 변수명)에 매몰되어 외부 계약과 상태 전이를 누락하지 않는다.

## 출력 형식 (Gate 0 입력용)

```text
## Feature Spec Blueprint
- Goal & Non-Goals: [목표 / 명시적 제외 범위]
- State Machine & Invariants:
  - States & Transitions: [상태 전이 요약]
  - Invariants: [절대 깨지면 안 되는 규칙]
- Edge Cases & Recovery:
  - [실패/경합/경계 시나리오] -> [기대 동작 및 복구]
- Pre-Agreed Acceptance TCs (Golden Set A Seed):
  - TC-01 [Happy|Edge|Error|E2E]: Given ... / When ... / Then ...
- Open Decisions (for Human): [0건 또는 정책 선택지 + 권장안]
```
