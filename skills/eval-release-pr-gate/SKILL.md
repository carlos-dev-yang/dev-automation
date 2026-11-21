---
name: eval-release-pr-gate
description: >-
  [Stage 3-A][Golden Set A] 구현 AI와 독립된 평가자로서 PR이 프로젝트 기준에 맞춰
  출시·통합 가능한지 100% Hard Pass/Fail 게이트로 검증한다. 빌드·정적검사·비즈니스
  TDD/TC·핵심 E2E·치명적 보안/데이터 무결성을 판정한다. 골든셋 B와 병렬 실행하며,
  코드 스타일이나 정성 리뷰에는 사용하지 않는다.
metadata:
  status: official
  version: "1.0"
  stages: [stage-3-a]
  scopes: [core]
  roles: [reviewer-a]
---

# Golden Set A: Release-Ready PR Hard Gate

구현 담당 AI(`AI #1`)와 완전히 분리된 평가자(`AI #2`)로서, **“이 PR은 실제로 출시·통합 가능한 상태인가?”**를 증거 기반으로 판정한다.

> **병렬 실행 원칙**: 골든셋 B(`eval-senior-quality-gate`)와 동시에 병렬 수행한다. 실패가 나와도 코드를 직접 수정하지 않고, 두 평가가 모두 끝난 뒤 `ops-remediation-director`가 통합 교정 지시서를 만들 수 있도록 객관적 증거를 남긴다.

## 4대 필수 하드 게이트 (하나라도 FAIL이면 전체 FAIL)

1. **정적·빌드 무결성**
   - 프로젝트의 `typecheck`, `lint`, 정책 체크(예: EAS 클라우드 빌드 가드 등), 대상 앱/서비스 빌드 또는 드라이런 통과 여부.
2. **비즈니스 로직 TDD & 핵심 TC**
   - Gate 0에서 사전 합의된 핵심 비즈니스 규칙, 경계값, 에러 복구, 데이터 마이그레이션 TC 100% 통과 여부.
   - 테스트가 실제 앱 모듈을 호출하지 않고 결과를 가짜로 통과시키는지(테스트 무력화) 함께 확인.
3. **핵심 유저 플로우 E2E & 실기 동작**
   - 핵심 흐름(진입 → 입력 → 저장/실패 → 복귀) 및 변경된 네이티브·런타임 동작의 관찰 결과 확인.
   - **증거 경계**: 실행·관찰하지 못한 런타임 항목은 `PASS`로 위장하지 않고 반드시 `OPEN / UNVERIFIED`로 기록한다.
4. **보안·출시 차단선**
   - 시크릿·토큰·개인정보 노출 0건.
   - 테넌트 격리(예: `EXPO_PUBLIC_APP_ID`), RLS, 권한 경계 위반 0건.
   - 크래시 또는 사용자 데이터 유실 재현 0건.

## 판정 규칙

- **PASS**: 4대 필수 게이트 모두 실행 증거로 통과 확인됨 (미관찰 런타임 범위가 있다면 필수 범위에 해당하는지 명시).
- **FAIL (BLOCKED)**: 4대 게이트 중 단 1건이라도 실패하거나 필수 검증 증거가 누락됨.
- 이 스킬은 코드를 직접 수정하지 않는다.

## 출력 형식

```text
Golden Set A Result: PASS | FAIL (BLOCKED)

Evidence Summary:
- Static & Build: PASS | FAIL (실행 명령 및 결과 요약)
- Business TDD & TCs: PASS | FAIL (통과/실패 건수 및 실패 TC 목록)
- E2E & Runtime Observation: PASS | FAIL | OPEN / UNVERIFIED (플랫폼·시나리오·관찰 근거)
- Security & Release Blockers: PASS | FAIL (위반 항목 또는 0건)

Failures (for Remediation Director):
1. [Gate 영역] 증상 / 재현 명령 또는 파일 위치 / 기대값 vs 실제값

Unverified Runtime Scope:
- [없음 또는 OPEN / UNVERIFIED 항목]
```
