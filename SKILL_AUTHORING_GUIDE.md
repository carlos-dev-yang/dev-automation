# Skill Authoring & Evolution Guide

이 문서는 `rd-skills` 저장소에서 스킬을 작성, 검증, `draft` 운영, `official` 승격, 그리고 클린 재작성(Rewrite)하는 표준 규범입니다.

## 1. 파일 구조 규범

모든 스킬은 `skills/<skill-name>/` 아래에 단일 플랫 구조로 배치합니다.

```text
skills/<skill-name>/
├── SKILL.md               # 필수: YAML frontmatter + 핵심 실행 지침 (기본은 이 파일 1개만 유지)
├── references/            # 선택: 상세 사례, 체크리스트, 템플릿 (본문 비대화 방지)
├── scripts/               # 선택: 결정론적 검증·보조 스크립트
└── agents/                # 예외 선택: Codex UI 표시나 자동 호출 차단 등 특수 정책이 필요할 때만 openai.yaml 추가
```

* `<skill-name>`은 소문자 kebab-case(`^[a-z0-9]+(-[a-z0-9]+)*$`)를 사용하며, 접두어로 역할을 바로 식별할 수 있게 명명하는 것을 권장합니다:
  * Stage 1 설계·게이트: `design-*`, `gate-*`
  * Stage 2 구현·도구: `impl-*`, `tool-*` (또는 기존 고유 명칭 유지)
  * Stage 3 골든셋 평가: `eval-*`
  * Stage 4 교정·운영: `ops-*`

---

## 2. 표준 YAML Frontmatter 규격 (유일한 인덱스)

별도의 `skills-index.yaml`을 두지 않으므로, 모든 AI가 자동 스캔하는 `SKILL.md` frontmatter가 곧 인덱스입니다.

```yaml
---
name: eval-senior-quality-gate
description: >-
  [Stage 3-B][Reviewer] 구현 AI와 독립된 시니어 개발자 관점으로 코드 품질,
  아키텍처 경계, 보안, 스킬 준수 여부를 심각도 기반 과락제(CRITICAL FAIL /
  MEDIUM·LOW Advice)로 평가한다. 골든셋 B 병렬 리뷰 시 사용.
metadata:
  status: official           # official | draft | deprecated
  version: "1.0"             # 개정 시 메이저/마이너 버전 갱신
  stages: [stage-3-b]        # stage-1 | stage-2 | stage-3-a | stage-3-b | stage-4 (비개발 스킬은 생략 가능)
  scopes: [core]             # core | stack:rn | stack:web | stack:server | stack:db | general | project:<repo>
  roles: [reviewer-b]        # architect | implementer | reviewer-a | reviewer-b | director (비개발 스킬은 생략 가능)
---
```

### 모든 스킬 일원화 및 스크립트 배치 원칙
개별 프로젝트의 `.agents/skills`, `.claude/skills`는 모두 이 저장소의 `skills/`로 단일 심볼릭 링크되므로, **공용·스택·비개발(`general`)·특정 프로젝트 전용(`project:<repo>`) 스킬을 물리 폴더로 나누지 않고 모두 `skills/`에 일원화**합니다.
* **오동작 차단**: 특정 프로젝트 전용이나 비개발 스킬은 `metadata.scopes`에 `project:<repo>` 또는 `general`을 명시하고, `description`에 활성화 조건(예: 특정 파일/스크립트 존재 여부, 코드 작업 비사용)을 분명히 적어 타 작업 간섭을 막습니다.
* **스크립트 및 실행 요건 배치 구분**:
  * **스킬 자체에 속한 범용/비개발 보조 스크립트**: `skills/<skill-name>/scripts/` 안에 함께 두어 어느 프로젝트에서나 심볼릭 링크로 바로 실행합니다.
  * **특정 프로젝트의 코드·인프라에 묶인 스크립트**: 해당 프로젝트 레포에 그대로 두고, 중앙의 `SKILL.md`는 깨지는 `../../../` 상대 마크다운 링크 대신 **현재 작업 디렉터리(CWD) 기준 경로**(예: `` `scripts/beta-feedback/README.md` ``)로 참조합니다.
  * **추가 개발 환경·런타임 요건**: 스킬 실행에 특정 CLI·런타임·패키지 설치가 필요하면 `SKILL.md`의 사전 요건(Prerequisites)에 명시하고, 레포 종속 조건은 `description`에 분명히 적습니다.

### 필수 작성 원칙
1. **`name`**: 반드시 폴더명(`<skill-name>`)과 정확히 일치해야 합니다.
2. **`description`**: AI가 이 스킬을 언제 켜고 언제 끄는지 결정하는 가장 중요한 필드입니다.
   * `[Stage ...]` 또는 `[DRAFT]` 프리픽스를 앞에 붙여 탐색 정확도를 높입니다.
   * **사용 시점(When to use)**과 **비사용 시점(Do NOT use when)**을 명확히 적습니다.
3. **`metadata`**: Agent Skills 표준 확장 필드를 사용해 단계·범위·역할·상태를 구조화합니다. 향후 스킬이 수백 개 이상으로 늘어날 경우 이 필드를 자동 파싱해 검색 CLI를 생성합니다.

---

## 3. Draft 운영 및 Official 승격 규칙

물리적 `drafts/` 폴더를 분리하지 않고, 단일 `skills/` 폴더와 Git PR로 운영합니다.

### Case A: 신규 스킬 제안/실험 (`status: draft`)
1. `skills/<new-skill>/SKILL.md`에 생성하고 `metadata.status: draft` 및 `description` 맨 앞에 `[DRAFT]`를 표기합니다.
2. PR을 올려 사람이 확인 후 머지하면, 심볼릭 링크를 통해 프로젝트에서 기존 정식 스킬과 함께 실전 테스트됩니다.
3. **승격**: 실전에서 유용함이 검증되면 `metadata.status: official`로 바꾸고 `[DRAFT]` 표기를 제거합니다.
4. **폐기**: 효과가 없거나 부작용이 있으면 해당 폴더를 삭제(또는 `git revert`)합니다.

### Case B: 기존 `official` 스킬의 교정/개정
1. **기본 방식 (Git Draft PR = 스테이징)**: 기존 `skills/<skill-name>/SKILL.md`를 직접 교정한 **독립 PR(Draft PR)**을 올립니다. 사람이 diff와 실패 근거를 검토해 승인·머지하면 즉시 전 프로젝트에 반영되며, 문제 발생 시 `git revert`로 즉시 복구합니다.
2. **예외 방식 (장기 병행 실험 필요 시)**: 기존 정식 스킬과 전면 대조가 필요한 대규모 개정에 한해 `skills/<skill-name>-next/SKILL.md` (`metadata.status: draft`)로 병행 테스트한 뒤, 검증 완료 시 기존 스킬에 통합하고 `-next`를 제거합니다.

---

## 4. 컨텍스트 비대화 방지 — 클린 재작성(Clean Rewrite) 원칙

피드백이 들어올 때마다 스킬 맨 아래에 `"추가 주의사항 17번: ..."` 식으로 덧붙이면 스킬이 금방 오염되고 AI 이행률이 떨어집니다.

1. **덧붙이기 금지 → 클린 재작성**: 스킬을 개정할 때는 기존 규칙의 빈틈과 신규 규칙을 구조적으로 합쳐 **해당 섹션 또는 전체 문서를 깔끔하게 재작성**하고 `metadata.version`을 올립니다.
2. **본문 길이 상한**: `SKILL.md` 본문은 핵심 원칙·절차·출력 형식 위주로 **150줄 이내**를 권장합니다. 구체적 실패 사례, 긴 체크리스트, 언어별 예시는 `references/*.md`로 분리해 필요할 때만 읽게 합니다(Progressive Disclosure).
3. **중복·충돌 점검**: 규칙을 추가하기 전에 기존 규칙 중 모순되거나 중복되는 문장이 없는지 반드시 정리합니다.
4. **일반 상식 배제**: AI가 이미 잘 아는 범용 코딩 상식은 쓰지 않고, **실제 프로젝트에서 실패했거나 반드시 강제해야 하는 고유 기준**만 담습니다.

---

## 5. PR 체크리스트 (스킬 개정 시 필수)

스킬을 추가하거나 개정하는 PR에는 아래 항목을 포함합니다.
- [ ] 발단된 실패 근거(골든셋 A/B 리포트 요약 또는 실제 사례)가 명시되어 있는가?
- [ ] 예외 문장 단순 덧붙이기가 아니라 기존 구조에 맞춰 클린 재작성되었는가?
- [ ] 기존 스킬과 충돌하거나 기준을 부당하게 완화(Reward Hacking)하지 않았는가?
- [ ] `node scripts/validate-skills.mjs` 검증을 통과했는가?
