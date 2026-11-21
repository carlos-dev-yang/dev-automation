---
name: gate-blueprint-check
description: >-
  [Stage 1][Gate 0] 개발 착수 직전 요구사항·아키텍처·피처·UI/UX·데이터 청사진의
  충돌과 누락을 점검하고, 미결 의사결정(Open Decisions)을 닫아 자율 구현 로드맵과
  골든셋 A 핵심 TC 초안을 확정한다. 코드 구현 시작 전 최종 청사진 검증 시 사용.
  단순 오타 수정이나 이미 승인된 로드맵의 중간 구현에는 사용하지 않는다.
metadata:
  status: official
  version: "1.0"
  stages: [stage-1]
  scopes: [core]
  roles: [architect]
---

# Gate 0: Blueprint Check & Autonomous Roadmap

개발(Stage 2)에 들어가기 전에 청사진의 빈틈을 제거해, 구현 AI가 작업 도중 멈추거나 임의로 스펙을 추측하지 않도록 고정한다.

## 점검 절차

1. **요구사항·경계 교차 검증**
   - 아키텍처(모듈·의존 방향), 피처(상태 전이·예외·복구), UI/UX(행동 위계·빈/로딩/오류 상태), 데이터(스키마·보존·마이그레이션·테넌트 격리) 간 모순이나 누락을 찾는다.
   - 프로젝트의 기존 루트/앱 `AGENTS.md` 및 핵심 경계 문서(예: `rn-foundation` 경계, 보안·배포 정책)와 충돌하는지 확인한다.
2. **미결 의사결정(Open Decisions) 추출**
   - 구현 중 AI가 임의로 판단하면 위험한 항목(데이터 삭제 범위, 실패 시 롤백 정책, 권한·결제·외부 API 경계, 핵심 UX 흐름)을 목록화해 사용자에게 결정을 요청한다.
   - 사소한 내부 구현 디테일로 사용자를 방해하지 않는다.
3. **자율 구현 로드맵(Execution Slices) 고정**
   - 구현 작업을 독립적으로 검증 가능한 Slice 단위로 나누고 순서, 소유 범위, 적용할 Stage 2 스킬, 완료 조건(DoD)을 명시한다.
4. **골든셋 A 핵심 TC 초안 고정**
   - 구현 전에 미리 합의된 핵심 비즈니스 TDD/TC 및 E2E 통과 기준을 적어, 사후에 구현에 맞춰 테스트를 짜는 편향을 막는다.

## 통과 기준 (Gate 0)

- **PASS (READY TO BUILD)**: 미결 의사결정(Open Decisions)이 0건이고, 모든 Slice의 완료 조건과 골든셋 A 핵심 TC가 확정됨.
- **BLOCKED (NEEDS ALIGNMENT)**: 결정되지 않은 핵심 스펙이나 청사진 간 충돌이 1건 이상 남아 있음. 이 상태에서는 Stage 2 구현을 시작하지 않는다.

## 출력 형식

```text
Result: PASS (READY TO BUILD) | BLOCKED (NEEDS ALIGNMENT)

1. Blueprint Conflicts & Gaps:
   - [없음 또는 항목별 충돌 내용과 해소 방안]

2. Open Decisions Requiring User Input:
   - [0건 또는 사용자 결정이 필요한 핵심 선택지]

3. Autonomous Implementation Roadmap (Stage 2):
   - Slice 1: [목표 / 대상 파일·모듈 / 주력 스킬 / DoD]
   - Slice 2: ...

4. Pre-Agreed Golden Set A Acceptance TCs:
   - [핵심 비즈니스 TC / 예외·경계값 TC / E2E 시나리오]
```
