# rd-skills

멀티 AI(Antigravity/Gemini, OpenAI Codex, Claude Code, Grok 등)가 R&D 프로젝트 전반에서 공통으로 사용하는 **중앙 스킬 저장소**입니다.

모든 스킬은 Agent Skills 오픈 스탠다드(`skills/<skill-name>/SKILL.md`)를 따르는 **단일 플랫 폴더**로 관리하며, 개별 프로젝트는 이 저장소의 `skills/` 폴더를 심볼릭 링크로 연결해 변경·승격 사항을 즉시 반영합니다.

## 핵심 문서

- [ARCHITECTURE.md](./ARCHITECTURE.md) — Stage 1~4 파이프라인, 병렬 이중 골든셋, **스킬 선교정 → 작업 후교정** 피드백 루프, Go 하네스·CLI 계약, 디자인 시스템 지연 결정(Deferred ADR)
- [docs/DECISION_HISTORY.md](./docs/DECISION_HISTORY.md) — 모든 핵심 의사결정(D-01 ~ D-13)과 기각된 대안·근거 기록 *(다른 AI 참여 시 필독)*
- [docs/WORK_HISTORY.md](./docs/WORK_HISTORY.md) — 단계별 작업 히스토리, 검증 현황 및 다른 머신 이어하기 체크리스트
- [docs/PROJECT_SETUP_GUIDE.md](./docs/PROJECT_SETUP_GUIDE.md) — 외부 프로젝트(및 외부 AI 스레드)에 중앙 스킬을 연결·이관·라우팅하는 표준 세팅 가이드
- [harness/README.md](./harness/README.md) — Go 얇은 워크플로우 하네스(`rd-harness`) 빌드·로컬 테스트 실행기·Telegram Long-Polling 운영 가이드
- [SKILL_AUTHORING_GUIDE.md](./SKILL_AUTHORING_GUIDE.md) — `SKILL.md` 표준 Frontmatter 규격, `draft` → `official` 승격 규칙, 클린 재작성(Rewrite) 원칙
- [AGENTS.md](./AGENTS.md) — 이 저장소 자체를 수정·관리하는 AI 에이전트 지침
- [templates/PROJECT_AGENTS_ROUTING.md](./templates/PROJECT_AGENTS_ROUTING.md) — 개별 프로젝트의 `AGENTS.md` / `CLAUDE.md`에 붙여 넣는 단계별 주력 스킬 라우팅 템플릿

## 디렉터리 구조

```text
rd-skills/
├── README.md
├── AGENTS.md
├── ARCHITECTURE.md
├── SKILL_AUTHORING_GUIDE.md
├── docs/
│   ├── DECISION_HISTORY.md
│   ├── PROJECT_SETUP_GUIDE.md
│   └── WORK_HISTORY.md
├── harness/                      # Go 얇은 워크플로우 하네스 + 로컬 실행기 + Telegram Gateway
├── templates/
│   └── PROJECT_AGENTS_ROUTING.md
├── scripts/
│   ├── validate-skills.mjs       # Frontmatter·메타데이터·링크 정합성 검증
│   └── link-project.mjs          # 대상 프로젝트에 .agents/skills, .claude/skills 심볼릭 링크 연결
└── skills/                       # 단일 플랫 구조 — core, stack:*, general, project:* 전부 일원화
    ├── design-data-model/        # (official 코어 10종 + status:draft 이관·실험 스킬)
    ├── design-feature-spec/
    ├── design-system-arch/
    ├── design-ui-ux/
    ├── eval-release-pr-gate/
    ├── eval-senior-quality-gate/
    ├── gate-blueprint-check/
    ├── impl-autonomous-slice/
    ├── ops-remediation-director/
    ├── ops-skill-evolution/
    └── ...                       # 각 스킬 SKILL.md frontmatter가 유일한 인덱스(SSoT)
```

## 빠른 시작

### 1. 스킬 정합성 검증

```bash
node scripts/validate-skills.mjs
```

### 2. 개별 프로젝트에 심볼릭 링크 연결

`dev-automation`은 항상 모든 대상 프로젝트와 **같은 부모 폴더 레벨(형제 디렉터리)**에 배치합니다.
상세 이관·Git 등록 절차는 [docs/PROJECT_SETUP_GUIDE.md](./docs/PROJECT_SETUP_GUIDE.md)를 참고하세요.

먼저 `--dry-run`으로 형제 디렉터리 배치와 기존 `.agents/skills`, `.claude/skills` 충돌 여부를 확인합니다.

```bash
node scripts/link-project.mjs --project ../<target-project> --dry-run
```

기존 `.agents/skills`, `.claude/skills` 실폴더의 스킬을 별도로 이관하고 폴더를 비운 뒤 실제 심볼릭 링크(`../../dev-automation/skills`)를 생성합니다. 생성된 심볼릭 링크는 대상 프로젝트의 Git에 등록(커밋)해 둡니다.

```bash
node scripts/link-project.mjs --project ../<target-project>
```

> 개별 프로젝트는 루트 `AGENTS.md`(및 필요 시 `CLAUDE.md`)에 [templates/PROJECT_AGENTS_ROUTING.md](./templates/PROJECT_AGENTS_ROUTING.md)의 라우팅 표를 배치해 각 AI가 작업 단계별 주력 스킬을 자동으로 선택하게 합니다.
