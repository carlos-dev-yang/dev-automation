---
name: impl-autonomous-slice
description: >-
  [Stage 2][Implementer] Gate 0에서 확정된 청사진과 로드맵에 따라 Slice 단위로 자율
  구현을 수행할 때 따르는 공통 실행 규율이다. 필수 스킬 바인딩, 전역 중단 조건(Circuit
  Breaker), CLI-Operable 테스트 구현, 예외 판단 로그 및 스킬 준수 증거 기록에 사용.
  Stage 1 설계나 Stage 3 골든셋 최종 판정에는 사용하지 않는다.
metadata:
  status: official
  version: "1.0"
  stages: [stage-2]
  scopes: [core]
  roles: [implementer]
---

# Stage 2: Autonomous Slice Implementation Discipline

Gate 0을 통과한 로드맵을 사람의 불필요한 개입 없이 완주하되, 잘못된 방향으로 억지 완성하거나 스킬을 건너뛰는 문제를 구조적으로 차단한다.

## 진입 선행 조건

- Stage 1 `gate-blueprint-check` 결과가 `PASS (READY TO BUILD)`인지 확인한다. 미결 의사결정이 남아 있으면 구현을 시작하지 않는다.
- 현재 Slice에 배정된 스택/도메인 스킬과 대상 프로젝트의 `AGENTS.md`를 **코드 작성 전에 먼저 로드**한다.

## 실행 규율

1. **Slice 단위 완결 구현**
   - 로드맵에 정의된 Slice 순서와 허용 변경 경계(Blast Radius) 안에서만 수정한다.
   - 인터페이스·데이터 계약 변경이 포함된 선행 Slice는 다음 Slice로 넘어가기 전에 타입·빌드·핵심 테스트가 통과하는 상태를 만든다.
2. **Pre-Agreed TC 구현 및 CLI-Operable 검증**
   - Gate 0에서 사전 합의된 비즈니스 TDD/TC를 실제 대상 모듈을 호출하는 유효한 테스트로 구현한다.
   - 구현한 기능의 빌드·정적검사·테스트·스모크 확인을 CLI 명령으로 재현 실행하고 실제 결과를 확인한다.
3. **Happy Path 침묵 · 예외 판단만 기록 (Deviation Log)**
   - 청사진대로 진행한 과정은 장황하게 기록하지 않는다.
   - 청사진의 사소한 빈틈을 메우며 내린 국소 판단(예: 경미한 타입 보강, 보조 유틸 위치 선택)만 1줄씩 예외 판단 로그로 남긴다.
4. **스킬 준수 증거(Compliance Evidence) 남기기**
   - 로드한 각 스킬의 핵심 제약이 실제 어느 코드(`path/to/file#L10-L25`)에 반영되었는지 요약해 골든셋 B(`eval-senior-quality-gate`)가 교차 검증할 수 있게 한다.

## 전역 중단 조건 (Global Stop Policy — 즉시 중단 및 보고)

아래 4가지 중 하나라도 발생하면 추측·우회·억지 완성하지 않고 즉시 작업을 중단(`STATUS: ABORTED`)하고 사람에게 보고한다:

1. **청사진 전제 불일치**: Gate 0에서 사용하기로 한 기존 모듈·API·스키마·컴포넌트가 실제 코드베이스에 없거나 계약이 달라 스펙 변경이 필요할 때.
2. **허용 변경 경계 이탈**: 승인된 Slice 범위를 벗어나 공용 코어 패키지, 타 도메인 모듈, 전역 설정 파일을 수정해야만 구현 가능할 때.
3. **미승인 의존성·환경 변경**: 합의되지 않은 외부 패키지 설치, 권한 확장, 신규 시크릿/환경변수 추가가 필요할 때.
4. **동일 실패 2회 반복 (Stuck Loop)**: 같은 원인·테스트 실패가 2회 연속 해결되지 않을 때 (3회 이상 강행은 Reward Hacking 위험이 급증함).

## 핵심 금지사항 (Anti-Hacking & Role Boundary)

- 테스트만 통과시키기 위한 하드코딩, 목(mock) 우회, 예외 삼킴, 고정 타임아웃 꼼수를 금지한다.
- 구현 AI(`AI #1`)가 스스로 골든셋 A 또는 B를 최종 통과 판정하지 않는다. 구현 완료 후 독립된 `AI #2`(골든셋 A)와 `AI #3`(골든셋 B) 병렬 검증으로 넘긴다.

## 완료 보고 형식 (Stage 3 인계용)

```text
Status: READY FOR STAGE 3 PARALLEL GATES | ABORTED (STOP POLICY)

1. Completed Slices & Changed Paths:
   - Slice N: [목표 / 주요 변경 파일]

2. Loaded Skills & Compliance Evidence:
   - [스킬명]: [적용한 핵심 제약 -> path/to/file#L10-L25]

3. Local CLI Verification Run:
   - [실행 명령 및 통과/실패 요약]

4. Deviations / Stop Policy Trigger (if any):
   - [없음 또는 예외 판단 / 중단 사유와 선택지]
```
