---
name: design-ui-ux
description: >-
  [Stage 1][Architect] 화면 정보 위계, 핵심 행동 관계, 입력·전환 흐름, 빈/로딩/오류 상태,
  사용자 중심 문구를 설계한다. 신규 화면·사용자 플로우·인터랙션 변경 설계 시 사용.
  백엔드 전용 작업이나 이미 Gate 0을 통과한 코드 구현에는 사용하지 않는다.
metadata:
  status: official
  version: "1.0"
  stages: [stage-1]
  scopes: [core]
  roles: [architect]
---

# Stage 1: UI/UX Flow & Interaction Design

구현 전에 화면의 역할·행동 위계·예외 상태·문구를 합의해, Stage 2에서 임시 UI나 개발자 용어가 섞인 화면이 나오지 않도록 한다.

## 설계 절차

1. **기존 앱 시각 언어 및 플랫폼 경계 확인**
   - 대상 프로젝트/앱의 기존 UI 가이드 및 플랫폼 오너십 문서(예: `docs/rn-foundation-ui/README.md`, 기존 화면 패턴)를 먼저 확인한다.
   - 별도 승인이 없는 한 전사 공용 디자인 시스템을 임의로 선구축하지 않고, 대상 앱 고유의 시각 톤과 플랫폼 네이티브 관례를 존중한다.
2. **화면 위계 및 핵심 행동(Action Hierarchy) 정의**
   - 각 화면의 Primary Action(단 1개 지향), Secondary Action, Destructive Action의 우선순위와 시각·동선 위계를 정한다.
   - 불필요한 단계나 중복 확인 모달을 줄이고, 파괴적 행동(삭제·초기화·결제 등)에만 명확한 확인 장치를 둔다.
3. **입력·전환·피드백 흐름 고정**
   - 진입 경로, 폼 입력/유효성 피드백 시점(인라인 vs 제출 시), 완료 후 복귀/이동 경로, 뒤로가기·중단 시 입력 보존 정책을 명시한다.
   - 키보드, 안전 영역(Safe Area), 터치/포커스 접근성, 반응형/화면 크기 변화에 따른 레이아웃 제약 조건을 적는다.
4. **필수 4대 상태(State Coverage) 정의**
   - **Empty(빈 상태)**: 첫 진입/데이터 없음 시 다음 행동을 유도하는 안내.
   - **Loading(로딩)**: 레이아웃 점프 방지 및 중복 탭 차단 기준.
   - **Error(오류)**: 원인 파악과 즉시 재시도/우회 가능한 복구 행동.
   - **Partial/Offline(부분 실패·오프라인)**: 기존 표시 데이터 유지 여부 및 동기화 상태 표시.
5. **사용자 중심 문구(Copy) 기준 고정**
   - 내부 시스템 용어, 코드명, 기계적 오류 문구 노출을 금지하고, 사용자가 취할 행동이 드러나는 간결한 문구로 정의한다.

## 금지사항

- 근거 없는 개인 시각 취향이나 임의의 새 디자인 토큰 체계 도입을 스펙에 섞지 않는다.
- Happy Path 화면만 그리고 Empty / Loading / Error 상태를 구현 AI의 재량에 맡기지 않는다.

## 출력 형식 (Gate 0 입력용)

```text
## UI/UX Blueprint
- Visual & Platform Guardrails: [준수할 기존 앱 톤 / 플랫폼 경계 문서]
- Screen & Action Hierarchy:
  - [화면명]: 목적 | Primary Action | Secondary/Destructive Action
- Navigation & Input Flow: [진입 -> 입력/검증 -> 완료/복귀 -> 중단 시 보존]
- Required UI States: [Empty / Loading / Error / Offline 처리 기준]
- Copy & Accessibility Rules: [핵심 사용자 문구 기준 / 접근성·레이아웃 제약]
- Open Decisions (for Human): [0건 또는 UX 선택지 + 권장안]
```
