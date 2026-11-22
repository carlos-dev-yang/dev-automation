---
name: design-data-model
description: >-
  [Stage 1][Architect] DB 스키마, 로컬/서버 동기화 계약, 테넌트·권한 격리(RLS),
  마이그레이션·롤백 및 데이터 보존 정책을 설계한다. 데이터 구조 변경, 영속 저장소,
  동기화 또는 마이그레이션이 포함된 작업 설계 시 사용. 데이터 변경 없는 순수 UI 작업이나
  이미 Gate 0을 통과한 코드 구현에는 사용하지 않는다.
metadata:
  status: official
  version: "1.0"
  stages: [stage-1]
  scopes: [core]
  roles: [architect]
---

# Stage 1: Data Model, Persistence & Migration Design

데이터 손실, 테넌트 혼선, 비가역 마이그레이션 장애를 원천 차단하기 위해 구현 전에 저장·동기화·격리 계약을 확정한다.

## 설계 절차

1. **엔티티·관계 및 단일 원본(SSoT) 고정**
   - 핵심 엔티티, 식별자 전략, 필수 제약(Unique, Not Null, Foreign Key, Enum/Check), 인덱스 대상을 정의한다.
   - 로컬 캐시/스토리지와 서버 DB가 공존할 경우 어느 쪽이 원본(SSoT)인지 명시한다.
2. **테넌트 격리·권한(RLS) 및 개인정보 경계**
   - 멀티 앱/멀티 테넌트 환경에서 데이터 격리 키(예: `app_id`, `tenant_id`, `user_id`)와 읽기/쓰기 권한 경계(RLS 또는 서버 검증)를 명시한다.
   - 개인정보·시크릿·민감 필드의 저장 위치, 마스킹/암호화 여부, 로그 출력 금지 대상을 지정한다.
3. **쓰기 원자성·동시성 및 동기화 계약**
   - 여러 테이블/파일을 갱신할 때 원자성(트랜잭션 또는 Atomic Write) 보장이 필요한 지점을 표시한다.
   - 오프라인/재시도 환경에서의 멱등성(Idempotency) 키와 충돌 해결 정책(예: LWW, 버전 체크, 병합)을 확정한다.
4. **마이그레이션·하위 호환·보존/삭제 정책**
   - 기존 데이터에 대한 무손실 마이그레이션 절차(Expand → Migrate → Contract 여부)와 실패 시 롤백 전략을 작성한다.
   - 삭제 정책(Soft Delete vs Hard Delete), 연관 데이터 정리(Cascade/Orphan 방지), 보존 기간을 명시한다.
5. **검증용 데이터 TC 초안 도출**
   - 마이그레이션 정합성, 권한/테넌트 격리 차단, 동시 쓰기/중복 요청 멱등성 검증을 골든셋 A TC 초안으로 넘긴다.

## 금지사항

- 기존 운영/사용자 데이터의 호환성이나 롤백 경로 없이 파괴적 스키마 변경(컬럼 삭제·타입 강제 변경)을 넣지 않는다.
- 권한/테넌트 격리를 클라이언트 필터링에만 의존하도록 설계하지 않는다.

## 출력 형식 (Gate 0 입력용)

```text
## Data Model Blueprint
- Entities & SSoT: [엔티티 / 키·제약·인덱스 / 로컬-서버 SSoT]
- Security, Tenant & RLS Boundaries: [격리 키 / 권한 정책 / 민감정보 보호]
- Atomicity, Concurrency & Sync: [트랜잭션 경계 / 멱등성 / 충돌 해결]
- Migration, Rollback & Retention: [마이그레이션 단계 / 롤백 / 삭제·보존 정책]
- Data Verification TCs (Golden Set A Seed): [격리·마이그레이션·원자성 TC]
- Open Decisions (for Human): [0건 또는 데이터 정책 선택지 + 권장안]
```
