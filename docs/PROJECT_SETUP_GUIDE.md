# 외부 프로젝트 R&D 스킬 세팅 가이드 (Project Onboarding Guide)

이 문서는 **외부 프로젝트(및 해당 프로젝트의 AI 에이전트)**가 중앙 스킬 저장소 `dev-automation` (`rd-skills`)을 안전하게 연결하고, Stage 1~4 스킬 거버넌스를 바로 적용할 수 있도록 만든 표준 세팅 절차서입니다.

> **AI 에이전트 필독**: 세팅을 시작하기 전에 반드시 [docs/DECISION_HISTORY.md](./DECISION_HISTORY.md) (D-01 ~ D-11)와 [SKILL_AUTHORING_GUIDE.md](../SKILL_AUTHORING_GUIDE.md)의 핵심 원칙을 먼저 확인하세요. 이미 기각된 구조(수동 `skills-index.yaml`, 물리 `drafts/` 폴더, 벤더별 `agents/openai.yaml` 일괄 생성, 골든셋 B 점수제)를 대상 프로젝트에 새로 만들지 않습니다.

---

## 0. 전제 및 경로 확인

세팅 시 아래 두 경로를 확인합니다:
- **`<CENTRAL_REPO>`**: 중앙 스킬 저장소 로컬 경로 (예: `/Users/yangmyeongsu/dev/src/dev-automation`)
- **`<TARGET_PROJECT>`**: 스킬을 붙일 대상 프로젝트 루트 경로 (예: `/Users/yangmyeongsu/dev/src/app-factory`)

중앙 저장소에 현재 준비된 Stage 1~4 코어 공식 스킬 10종:
- **Stage 1 (설계·Gate 0)**: `design-system-arch`, `design-feature-spec`, `design-ui-ux`, `design-data-model`, `gate-blueprint-check`
- **Stage 2 (자율 구현)**: `impl-autonomous-slice`
- **Stage 3 (병렬 이중 골든셋)**: `eval-release-pr-gate` (A), `eval-senior-quality-gate` (B)
- **Stage 4 (통합 교정·스킬 진화)**: `ops-remediation-director`, `ops-skill-evolution`

---

## 1. 사전 충돌 점검 (`--dry-run` 필수)

먼저 파일을 수정하지 않고 대상 프로젝트의 `.agents/skills`, `.claude/skills` 충돌 여부를 확인합니다.

```bash
node <CENTRAL_REPO>/scripts/link-project.mjs \
  --project <TARGET_PROJECT> \
  --dry-run
```

결과별 분기:
- **`[DRY-RUN] Would create symlink`**: 충돌 없음 → 바로 **Step 3**으로 이동.
- **`[OK] Already linked`**: 이미 정상 연결됨 → **Step 4**로 이동.
- **`[CONFLICT] Real directory/file already exists`**: 대상 프로젝트에 기존 스킬 폴더가 있음 → 절대 덮어쓰거나 바로 삭제하지 말고 **Step 2**를 먼저 수행.

---

## 2. 기존 프로젝트 스킬 이관 및 정리 (`[CONFLICT]` 발생 시)

기존 `.agents/skills` 또는 `.claude/skills` 안에 이미 스킬이 있다면, 각 스킬의 `SKILL.md`를 검토해 아래 3가지로 분류합니다.

| 분류 | 판단 기준 | 처리 방식 |
| :--- | :--- | :--- |
| **A. 중앙 이관** | 다른 프로젝트에서도 재사용 가능한 스택·범용 개발 스킬 (예: RN/Expo, Web, Server, Supabase/DB 가이드) | `<CENTRAL_REPO>/skills/<kebab-name>/SKILL.md`로 이관.<br>- [SKILL_AUTHORING_GUIDE.md](../SKILL_AUTHORING_GUIDE.md) 규격(표준 YAML frontmatter, `metadata.status`, 150줄 이내 권장)으로 정비<br>- 특수 정책 없는 `agents/openai.yaml`·`grok.yaml` 제거(D-03)<br>- `<CENTRAL_REPO>`에서 `node scripts/validate-skills.mjs` 통과 확인 |
| **B. 프로젝트 문서화** | 특정 레포·앱에만 종속된 도메인 규칙, 앱 고유 시각 언어, 배포·환경 정책 (예: `docs/rn-foundation-ui/README.md`) | 중앙 스킬로 옮기지 않고 대상 프로젝트의 `AGENTS.md` 또는 `docs/`로 유지·정리 (D-08 준수) |
| **C. 중복·구버전 폐기** | 중앙 코어 10종과 역할이 겹치거나 더 이상 쓰지 않는 임시 스킬 | 사용자 확인 후 제거 |

> **안전 경계**: 기존 스킬 이관·삭제·중앙 레포 수정 내역은 실행 전 사용자에게 분류 표를 먼저 보고해 승인을 받은 뒤 진행합니다. 이관이 끝나고 기존 실폴더를 정리한 뒤에 Step 3으로 넘어갑니다.

---

## 3. 심볼릭 링크 연결

충돌이 해소되면 실제 심볼릭 링크를 생성합니다.

```bash
node <CENTRAL_REPO>/scripts/link-project.mjs \
  --project <TARGET_PROJECT>
```

연결 후 두 경로 모두 중앙 스킬 10종(+ 이관된 스킬)이 정상 노출되는지 확인합니다:

```bash
ls -la <TARGET_PROJECT>/.agents/skills <TARGET_PROJECT>/.claude/skills
ls -L <TARGET_PROJECT>/.agents/skills
```

### Git 추적 기준 (중요)
- `link-project.mjs`는 대상 프로젝트 기준 **상대 경로 심볼릭 링크**(예: `../../dev-automation/skills`)를 생성합니다.
- **권장**: 개인/팀원마다 중앙 저장소 클론 위치가 다를 수 있으므로, 별도 합의가 없다면 대상 프로젝트 `.gitignore`에 `.agents/skills`와 `.claude/skills`를 등록하고 각 개발 환경에서 `link-project.mjs`를 실행하게 합니다.
- 모든 환경이 동일한 상위 디렉터리 구조를 고정 사용하는 경우에만 심볼릭 링크 자체를 커밋합니다.

---

## 4. 대상 프로젝트 `AGENTS.md` / `CLAUDE.md` 라우팅 반영

1. [templates/PROJECT_AGENTS_ROUTING.md](../templates/PROJECT_AGENTS_ROUTING.md)의 `## R&D Skill Governance & Stage Routing` 섹션을 대상 프로젝트 루트 `AGENTS.md`에 반영합니다.
2. **기존 프로젝트 규칙 보존**: 대상 프로젝트 `AGENTS.md`에 이미 적혀 있는 프로젝트 고유 아키텍처, 보안, 앱별 경계, 빌드/배포 규칙은 지우지 말고 유지합니다.
3. **Stage 2 스택 스킬 이름 동기화**: 라우팅 표의 Stage 2 행에 이 프로젝트에서 실제로 사용할 스택 스킬명(Step 2에서 이관한 스킬 포함)을 정확히 기입합니다.
4. **`CLAUDE.md` 처리**: 대상 프로젝트에 `CLAUDE.md`가 이미 있으면 중복 내용을 복붙해 Drift를 만들지 말고, `AGENTS.md`를 따르도록 짧은 포인터를 두거나 기존 프로젝트 관례에 맞춰 최소한으로 동기화합니다.

---

## 5. CLI-Operable 검증 명령 명시 (권장)

Stage 2(`impl-autonomous-slice`), Stage 3 골든셋 A(`eval-release-pr-gate`), 그리고 향후 Phase 2 Go 하네스(`rd-project.yaml`, D-10)가 GUI 수동 조작에 묶이지 않고 검증할 수 있도록, 대상 프로젝트 루트 `AGENTS.md`에 아래 결정론적 CLI 명령이 명시되어 있는지 점검합니다:
- `typecheck` / `lint` / 정책 체크 명령
- 단위·비즈니스 TDD/TC 실행 명령
- 빌드 검증(`build` 또는 dry-run) 및 스모크 E2E 실행 명령

---

## 6. 최종 검증 체크리스트

세팅을 마치기 전 아래 항목을 모두 확인합니다:
- [ ] `ls -L <TARGET_PROJECT>/.agents/skills`와 `.claude/skills`에서 중앙 스킬 목록이 정상 조회되는가?
- [ ] 중앙 저장소로 이관/추가한 스킬이 있다면 `<CENTRAL_REPO>`에서 `node scripts/validate-skills.mjs`가 오류 0건으로 통과하는가?
- [ ] 대상 프로젝트 루트 `AGENTS.md`에 Stage 1~4 주력 스킬 라우팅 표와 핵심 금지 원칙(자가 판정 금지, 전역 중단 조건, Fan-in 대기, Reward Hacking 금지)이 반영되었는가?
- [ ] `.agents/skills`, `.claude/skills`의 Git 추적 여부(`.gitignore` vs 커밋)가 프로젝트 정책과 충돌하지 않는가?
- [ ] Git commit / push / PR은 사용자가 명시적으로 승인한 범위에서만 수행했는가?

---

## 7. 외부 AI 스레드용 바로 실행 프롬프트 (복사용)

다른 프로젝트 스레드에서 이 가이드를 참조해 세팅을 진행할 때 아래 프롬프트를 그대로 붙여넣어 사용할 수 있습니다.

```text
/Users/yangmyeongsu/dev/src/dev-automation/docs/PROJECT_SETUP_GUIDE.md 가이드를 읽고,
현재 프로젝트에 중앙 R&D 스킬 저장소(/Users/yangmyeongsu/dev/src/dev-automation)를 세팅해줘.

진행 순서:
1. 먼저 `link-project.mjs --dry-run`과 현재 프로젝트의 `.agents/skills`, `.claude/skills`,
   루트 `AGENTS.md`/`CLAUDE.md` 현황을 파악해서 보고해줘. (파일 삭제·이관 전 내 확인 필수)
2. 기존 스킬이 있다면 중앙 이관 / 프로젝트 문서화 / 폐기 분류안을 제안해줘.
3. 내 승인 후 심볼릭 링크 연결, `AGENTS.md` 라우팅 반영, 최종 체크리스트 검증까지 완료해줘.
```
