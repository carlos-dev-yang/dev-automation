---
name: ops-skill-evolution
description: >-
  [Stage 4][Skill Curator] ops-remediation-director가 지목한 Type S(스킬 결함)를
  교정하거나, 신규 실험 스킬(status: draft) 생성, 기존 official 스킬의 클린 재작성
  PR 준비, draft에서 official 승격·폐기를 수행한다. 중앙 rd-skills 레포 갱신 시 사용.
  프로젝트 코드 버그 수정이나 골든셋 평가 실행에는 사용하지 않는다.
metadata:
  status: official
  version: "1.0"
  stages: [stage-4]
  scopes: [core]
  roles: [director]
---

# Stage 4: Skill Authoring, Clean Rewrite & Promotion

실패 사례가 일회성 수정으로 사라지지 않고 중앙 `rd-skills` 자산으로 축적되도록, 규범에 맞춰 스킬을 생성·교정·승격한다.

## 필수 사전 확인

작업 전 중앙 저장소의 아래 두 문서를 반드시 확인하고 위반하지 않는다:
- [docs/DECISION_HISTORY.md](../../docs/DECISION_HISTORY.md) — 확정 결정(D-01 ~ D-11) 준수, 기각된 대안(수동 인덱스, 물리 `drafts/` 폴더, 점수제 등) 재도입 금지.
- [SKILL_AUTHORING_GUIDE.md](../../SKILL_AUTHORING_GUIDE.md) — 플랫 구조, Frontmatter 규격, 클린 재작성 및 150줄 이내 원칙.

## 케이스별 실행 절차

### Case A. 신규 실험 스킬 생성 (`status: draft`)
1. `skills/<kebab-case-name>/SKILL.md` 단일 파일로 생성한다(`agents/openai.yaml` 등 벤더 파일은 특수 정책 없으면 만들지 않는다).
2. `metadata.status: draft`와 `description` 맨 앞에 `[DRAFT]` 프리픽스를 명시하고, 사용 시점과 비사용 시점을 분명히 적는다.
3. 일반 코딩 상식이나 장황한 Good/Bad 예시 코드 대신, 실제 실패를 막는 **명확한 원칙·금지사항·출력 계약**만 담는다.

### Case B. 기존 `official` 스킬 교정 (Clean Rewrite PR)
1. **덧붙이기(Append) 금지**: 기존 문서 맨 아래에 예외 조항을 추가하지 않는다. 기존 규칙의 빈틈과 신규 규칙을 통합해 해당 섹션 또는 전체를 **클린 재작성**하고 `metadata.version`을 올린다.
2. **기준 완화(Reward Hacking) 금지**: 골든셋을 통과시키기 위해 기존 필수 게이트나 안전 기준을 부당하게 약화하지 않는다.
3. **길이 및 충돌 점검**: 본문은 150줄 이내를 유지(긴 체크리스트·사례는 `references/*.md`로 분리)하고, 다른 스킬과의 중복·모순을 제거한다.
4. 기존 `official` 스킬 개정은 독립 브랜치/PR로 제안하며, 사람 승인 없이 직접 머지하지 않는다.

### Case C. `draft` → `official` 승격 또는 폐기
1. 실전 과제에서 유효성과 부작용 없음이 확인된 `draft` 스킬만 `metadata.status: official`로 변경하고 `[DRAFT]` 프리픽스를 제거한다.
2. 효과가 없거나 노이즈만 만드는 `draft` 스킬은 미련 없이 삭제(또는 `git revert`)한다.

## 필수 검증

스킬을 생성·수정·승격한 뒤에는 반드시 저장소 루트에서 아래 검증을 실행해 에러 0건을 확인한다:

```bash
node scripts/validate-skills.mjs
```

## PR / 보고 출력 형식

```text
Skill Evolution Action: [NEW DRAFT | CLEAN REWRITE PR | PROMOTE TO OFFICIAL | RETIRE]

1. Trigger Evidence:
   - [발단된 골든셋 A/B 리포트, ops-remediation-director RC-#, 또는 실전 사례]

2. Target Skill & Structural Change Summary:
   - [skills/<name>/SKILL.md]: [버전 변경 / 통합·재작성 핵심 내용]

3. Conflict & Anti-Relaxation Check:
   - [DECISION_HISTORY 및 타 스킬 충돌 0건 확인 / 기존 기준 부당 완화 없음 확인]

4. Validation Output:
   - `node scripts/validate-skills.mjs` 통과 결과
```
