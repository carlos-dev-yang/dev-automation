---
name: design-system-arch
description: >-
  [Stage 1][Architect] 신규 프로젝트·기능·리팩터링 착수 전 시스템 경계, 모듈 책임,
  의존 방향, 패키지 분리, 인프라·외부 연동 및 CLI-Operable 제어 경계를 설계한다.
  아키텍처·모듈 구조·공용 패키지 경계 논의 시 사용. 단순 UI 문구 수정이나 이미
  Gate 0을 통과한 코드 구현에는 사용하지 않는다.
metadata:
  status: official
  version: "1.0"
  stages: [stage-1]
  scopes: [core]
  roles: [architect]
---

# Stage 1: System & Module Architecture Design

구현 전에 시스템 경계와 의존 방향을 고정해, Stage 2 자율 구현 중 구조가 흔들리거나 변경 범위(Blast Radius)가 무단 확장되지 않게 한다.

## 설계 절차

1. **기존 경계 및 제약 스캔**
   - 대상 레포의 루트/앱 `AGENTS.md`, 기존 ADR, 패키지 의존 그래프, 플랫폼 경계 문서(예: `rn-foundation`, 보안·배포 정책)를 먼저 확인한다.
   - 기존 아키텍처와 충돌하는 변경이 필요하면 임의로 우회하지 말고 명시적 변경 제안(ADR)으로 올린다.
2. **책임 경계 및 의존 방향 고정**
   - 변경 대상 모듈·패키지의 단일 책임(소유자)과 **허용 의존 방향(Allowed Dependencies)** 및 **금지 의존(Forbidden Dependencies)**을 정의한다.
   - 멀티 앱/모노레포 환경에서는 앱 전용 로직과 공용 패키지 추출 대상을 구분해 중복 구현을 방지한다.
   - Stage 2 구현 AI가 건드려도 되는 **허용 변경 경계(Allowed Blast Radius)**와 수정 금지 영역을 명시한다.
3. **외부 연동·실패 격리 및 런타임 경계**
   - 외부 API, 스토리지, 네이티브 브리지, 인증/권한 경계마다 타임아웃·재시도·폴백·에러 전파 책임을 지정한다.
   - 시크릿·테넌트 격리·권한 검증이 실행되는 신뢰 경계(Trust Boundary)를 명확히 표시한다.
4. **CLI-Operable 검증 경로 정의**
   - 새로 도입하거나 변경하는 모듈이 GUI 수동 확인에만 묶이지 않도록, 빌드·정적검사·단위/통합 테스트·스모크 확인을 실행할 CLI 명령 경로를 명시한다.
5. **미결 의사결정(Open Decisions) 분리**
   - 트레이드오프가 크고 되돌리기 어려운 결정(신규 의존성 추가, 패키지 경계 변경, 호환성 단절 등)은 옵션·권장안과 함께 Open Decisions로 올려 사람 확정을 받는다.

## 금지사항

- 실제 코드베이스에 없는 모듈·API·공용 패키지를 있다고 가정해 청사진을 작성하지 않는다.
- 확정되지 않은 미래 요구사항을 위해 과도한 추상화 계층이나 공용 프레임워크를 선구축하지 않는다.

## 출력 형식 (Gate 0 입력용)

```text
## Architecture Blueprint
- Scope & Non-Goals: [이번 변경 범위 / 명시적 비목표]
- Module Boundaries & Dependencies:
  - [모듈/패키지]: 책임 | 의존 가능 대상 | 금지 의존
- Allowed Blast Radius: [수정 허용 경로] / Protected Paths: [수정 금지 경로]
- Trust & Failure Boundaries: [권한·테넌트 격리·외부 장애 처리 책임]
- CLI Verification Path: [정적검사 / 테스트 / 빌드 확인 명령]
- Open Decisions (for Human): [0건 또는 선택지 + 권장안]
```
