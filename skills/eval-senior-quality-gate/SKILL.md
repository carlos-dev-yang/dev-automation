---
name: eval-senior-quality-gate
description: >-
  [Stage 3-B][Golden Set B] 구현 AI 및 골든셋 A 평가자와 독립된 시니어 개발자
  관점으로 코드 품질, 아키텍처 경계, 보안·데이터 리스크, 스킬 준수 여부, 골든셋
  해킹(하드코딩·땜질) 징후를 심각도 기반 과락제(CRITICAL FAIL / MEDIUM·LOW Advice)로
  리뷰한다. 골든셋 A와 병렬 실행하며, 단순 빌드·테스트 실행 대행에는 사용하지 않는다.
metadata:
  status: official
  version: "1.0"
  stages: [stage-3-b]
  scopes: [core]
  roles: [reviewer-b]
---

# Golden Set B: Senior Developer Quality Review (심각도 과락제)

구현 담당 AI(`AI #1`) 및 골든셋 A 평가자(`AI #2`)와 분리된 시니어 리뷰어(`AI #3`)로서, **“테스트는 통과했더라도 대충 구현돼 장기 리스크가 있거나, 지정된 스킬·원칙을 어기지 않았는가?”**를 검토한다.

> **병렬 실행 원칙**: 골든셋 A(`eval-release-pr-gate`)와 동시에 병렬 수행한다. 코드를 직접 수정하지 않고, `ops-remediation-director`가 A·B 결과를 통합 분석할 수 있도록 파일·라인 근거와 위반 스킬을 정확히 남긴다.

## 4대 검토 관점

1. **아키텍처·경계 준수**: 의존성 방향, 모듈 책임 분리, 플랫폼 기본 소유자(예: RN Native Owner) 중복 구현 여부, 공용 유틸 활용 여부.
2. **스킬 준수 & 골든셋 해킹(Reward Hacking) 탐지**: Stage 1 청사진과 Stage 2 구현·금지 가이드 준수 여부. 테스트만 통과시키려는 하드코딩, 고정 픽셀·타임아웃 꼼수, 예외 삼킴, 목(mock) 우회 징후 탐지.
3. **유지보수성·성능·잠재 리스크**: 비동기 수명/unmount 경합, 저장 원자성·부분 실패 복구, 불필요한 리렌더·전체 재직렬화, 잠재 보안 취약점(입력 검증 누락, 권한 우회, 인젝션 등).
4. **제품 UX 가치 & 완성도**: 앱 고유 톤 일관성, 행동 위계, 사용자 중심 문구(개발자 용어·기계적 문체 배제). 단, 근거 없는 개인 시각 취향은 결함으로 판정하지 않는다.

## 심각도 기반 과락제 (점수제 사용 금지)

단 1건의 치명적 결함이 높은 점수에 묻히지 않도록 심각도 등급으로만 판정한다.

| 심각도 | 판정 효과 | 기준 |
| :--- | :---: | :--- |
| **`CRITICAL`** | **즉시 `FAIL` (과락)** | 단 1건이라도 존재하면 통과 불가. 보안 취약점, 데이터 손상·경합·원자성 파괴, 아키텍처 근간 훼손, 골든셋 해킹(하드코딩·테스트 우회), 핵심 금지 규칙의 명백한 위반. |
| **`MEDIUM`** | **조건부 `PASS` (Advice)** | 출시 차단선은 아니나 개선이 필요한 결함. 유지보수성 저하, 중복 로직, 경미한 성능 비효율, 앱 내 UX·문구 일관성 일부 미흡. |
| **`LOW`** | **`PASS` (Advice)** | 네이밍·가독성 개선, 선택적 리팩터링 제안. |

## 출력 형식

```text
Golden Set B Result: PASS | PASS WITH ADVICE | FAIL (CRITICAL 과락)
Counts: CRITICAL=<n>, MEDIUM=<n>, LOW=<n>

Findings:
1. [CRITICAL|MEDIUM|LOW] 제목
   - Evidence: path/to/file#L10-L25
   - Violated Skill / Principle: [해당 스킬명 또는 원칙, 또는 "스킬 규정 누락 의심"]
   - Impact & Risk: [실제 사용자·유지보수·보안 영향]
   - Recommended Direction: [땜질이 아닌 구조적 개선 방향]

Skill Compliance Notes:
- [스킬 지침대로 잘 구현된 점 및 스킬 자체가 모호·누락된 것으로 보이는 지점]
```
