# rd-skills

멀티 AI(Antigravity/Gemini, OpenAI Codex, Claude Code, Grok 등)가 R&D 프로젝트 전반에서 공통으로 사용하는 **중앙 스킬 저장소**입니다.

모든 스킬은 Agent Skills 오픈 스탠다드(`skills/<skill-name>/SKILL.md`)를 따르는 **단일 플랫 폴더**로 관리하며, 개별 프로젝트는 이 저장소의 `skills/` 폴더를 심볼릭 링크로 연결해 변경·승격 사항을 즉시 반영합니다.

## 핵심 문서

- [ARCHITECTURE.md](./ARCHITECTURE.md) — Stage 1~4 파이프라인, 병렬 이중 골든셋, **스킬 선교정 → 작업 후교정** 피드백 루프, Go 하네스·CLI 계약, 디자인 시스템 지연 결정(Deferred ADR)
- [docs/DECISION_HISTORY.md](./docs/DECISION_HISTORY.md) — 모든 핵심 의사결정(D-01 ~ D-11)과 기각된 대안·근거 기록 *(다른 AI 참여 시 필독)*
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
├── templates/
│   └── PROJECT_AGENTS_ROUTING.md
├── scripts/
│   ├── validate-skills.mjs       # Frontmatter·메타데이터·링크 정합성 검증
│   └── link-project.mjs          # 대상 프로젝트에 .agents/skills, .claude/skills 심볼릭 링크 연결
└── skills/                       # 단일 플랫 구조 (별도 index 파일 및 drafts 폴더 없음)
    ├── gate-blueprint-check/
    ├── eval-release-pr-gate/
    ├── eval-senior-quality-gate/
    └── ops-remediation-director/
```

## 빠른 시작

### 1. 스킬 정합성 검증

```bash
node scripts/validate-skills.mjs
```

### 2. 개별 프로젝트에 심볼릭 링크 연결

먼저 `--dry-run`으로 연결 대상과 기존 폴더 충돌 여부를 확인합니다.

```bash
node scripts/link-project.mjs --project /Users/carlos-yang/dev/app-automation --dry-run
```

기존 `.agents/skills`나 `.claude/skills`가 비어 있거나 이관을 마친 뒤 실제 링크를 생성합니다.

```bash
node scripts/link-project.mjs --project /Users/carlos-yang/dev/app-automation
```

> 개별 프로젝트는 루트 `AGENTS.md`(및 필요 시 `CLAUDE.md`)에 [templates/PROJECT_AGENTS_ROUTING.md](./templates/PROJECT_AGENTS_ROUTING.md)의 라우팅 표를 배치해 각 AI가 작업 단계별 주력 스킬을 자동으로 선택하게 합니다.
