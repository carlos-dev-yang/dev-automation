# 외부 프로젝트 R&D 스킬 세팅 가이드 (Project Onboarding Guide)

이 문서는 **외부 프로젝트(및 해당 프로젝트의 AI 에이전트)**가 중앙 스킬 저장소 `dev-automation` (`rd-skills`)을 안전하게 연결하고, Stage 1~4 스킬 거버넌스를 바로 적용할 수 있도록 만든 표준 세팅 절차서입니다.

> **AI 에이전트 필독**: 세팅을 시작하기 전에 반드시 [docs/DECISION_HISTORY.md](./DECISION_HISTORY.md) (D-01 ~ D-11)와 [SKILL_AUTHORING_GUIDE.md](../SKILL_AUTHORING_GUIDE.md)의 핵심 원칙을 먼저 확인하세요. 이미 기각된 구조(수동 `skills-index.yaml`, 물리 `drafts/` 폴더, 벤더별 `agents/openai.yaml` 일괄 생성, 골든셋 B 점수제)를 대상 프로젝트에 새로 만들지 않습니다.

---

## 0. 필수 폴더 배치 규약 (형제 디렉터리 고정)

**`dev-automation` 저장소는 항상 모든 대상 프로젝트와 같은 부모 폴더 레벨(형제 디렉터리)에 배치해야 합니다.**

```text
<workspace-root>/                    # 예: /Users/<user>/dev/src/
├── dev-automation/                  # 중앙 스킬 저장소 (디렉터리명 고정: dev-automation)
│   └── skills/                      # 단일 플랫 스킬 원본(SSoT)
├── app-factory/                     # 대상 프로젝트 A
│   ├── .agents/skills -> ../../dev-automation/skills   # Git 추적(등록) 필수
│   └── .claude/skills -> ../../dev-automation/skills   # Git 추적(등록) 필수
└── another-project/                 # 대상 프로젝트 B
    ├── .agents/skills -> ../../dev-automation/skills   # Git 추적(등록) 필수
    └── .claude/skills -> ../../dev-automation/skills   # Git 추적(등록) 필수
```

* **왜 형제 디렉터리 배치가 필수인가?**
  * 모든 프로젝트에서 심볼릭 링크 상대 경로가 예외 없이 **`../../dev-automation/skills`**로 동일하게 고정됩니다.
  * 이 심볼릭 링크를 **각 프로젝트 Git 저장소에 그대로 등록(커밋)**해 두면, 어느 환경에서든 같은 부모 폴더 아래에 `dev-automation`과 대상 프로젝트를 나란히 클론하는 즉시 모든 AI(Antigravity/Gemini, Codex, Claude Code, Grok 등)가 중앙 스킬셋을 자동 인식합니다.

현재 중앙 저장소에 준비된 Stage 1~4 코어 공식 스킬 10종:
- **Stage 1 (설계·Gate 0)**: `design-system-arch`, `design-feature-spec`, `design-ui-ux`, `design-data-model`, `gate-blueprint-check`
- **Stage 2 (자율 구현)**: `impl-autonomous-slice`
- **Stage 3 (병렬 이중 골든셋)**: `eval-release-pr-gate` (A), `eval-senior-quality-gate` (B)
- **Stage 4 (통합 교정·스킬 진화)**: `ops-remediation-director`, `ops-skill-evolution`

---

## 1. 사전 배치 및 충돌 점검 (`--dry-run` 필수)

먼저 `dev-automation`과 `<TARGET_PROJECT>`가 같은 부모 폴더 아래에 있는지 확인한 뒤, 파일을 수정하지 않고 충돌 여부를 점검합니다.

```bash
node ../dev-automation/scripts/link-project.mjs \
  --project . \
  --dry-run
```
*(또는 절대 경로로 실행: `node <PARENT>/dev-automation/scripts/link-project.mjs --project <PARENT>/<PROJECT> --dry-run`)*

결과별 분기:
- **`[DRY-RUN] Would create symlink: ... -> ../../dev-automation/skills`**: 배치 정상·충돌 없음 → 바로 **Step 3**으로 이동.
- **`[OK] Already linked`**: 이미 정상 연결됨 → **Step 3의 Git 등록 확인** 및 **Step 4**로 이동.
- **`[CONFLICT] Real directory/file already exists`**: 대상 프로젝트에 기존 `.agents/skills` 또는 `.claude/skills` 실폴더가 존재함 → **Step 2에서 기존 스킬을 별도 이관하고 폴더를 완전히 비운(삭제한) 뒤에야** 심볼릭 링크를 연결할 수 있음.
- **형제 디렉터리 불일치 오류**: `dev-automation`을 대상 프로젝트와 같은 부모 폴더 레벨로 이동·클론한 뒤 재실행.

---

## 2. 기존 스킬 별도 이관 및 `.agents/skills` 비우기 (필수 선행)

**대상 프로젝트의 `.agents/skills`(및 `.claude/skills`) 실폴더가 비워지고 제거되어야만 중앙 스킬셋 심볼릭 링크를 심어 사용할 수 있습니다.** 기존 폴더에 스킬이 들어 있다면 절대 바로 덮어쓰거나 무단 삭제하지 않고, 아래 3가지로 전수 분류해 별도 이관합니다.

| 분류 | 판단 기준 | 이관·정리 방식 |
| :--- | :--- | :--- |
| **A. 중앙 `skills/` 일원화 (`draft` 이관)** | **스킬 형태로 계속 쓸 모든 항목** — 공용 스택 스킬(`stack:*`), 비개발/일반 스킬(`general`), 특정 프로젝트 전용 운영 스킬(`project:<repo>`) 포함 | `../dev-automation/skills/<kebab-name>/SKILL.md`로 **먼저 `metadata.status: draft` + `[DRAFT]` 프리픽스**로 일괄 이관.<br>- [SKILL_AUTHORING_GUIDE.md](../SKILL_AUTHORING_GUIDE.md) 규격 적용, 특수 정책 없는 `agents/openai.yaml`·`grok.yaml` 제거(D-03)<br>- 프로젝트 전용 스킬은 `scopes: [project:<repo>]` 및 `description`에 해당 레포 조건 명시, 깨지는 `../../../` 마크다운 링크는 현재 워크스페이스(CWD) 기준 경로 표기로 교정<br>- 범용/비개발 스킬 자체 스크립트는 `skills/<name>/scripts/`에 동봉, 프로젝트 코드·DB에 묶인 스크립트는 대상 레포에 그대로 유지<br>- `dev-automation`에서 `node scripts/validate-skills.mjs` 오류 0건 확인 |
| **B. 프로젝트 경계 문서 유지 (비-스킬)** | 스킬이 아닌 레포·앱 고유 아키텍처/시각/배포 경계 문서 (예: `AGENTS.md`, `docs/rn-foundation-ui/README.md`) | 중앙 스킬로 옮기지 않고 대상 프로젝트 내부 문서로 그대로 유지 (D-08 준수) |
| **C. 동작 불가·중복 폐기** | 깨진 심볼릭 링크로 실행 불가하거나 중앙 코어 10종과 중복되어 불필요한 스킬 | 사용자 확인 후 제거 |

### 이관 완료 후 기존 폴더 비우기 절차
1. 위 분류표를 사용자에게 먼저 보고하고 이관·폐기 승인을 받는다.
2. **A(중앙 이관)**와 **B(프로젝트 문서화)** 작업을 모두 완료하고, 이관된 파일이 안전하게 반영되었는지 검증한다.
3. 대상 프로젝트의 기존 `.agents/skills` 및 `.claude/skills` 실폴더를 완전히 비우고 제거한다(`rm -rf .agents/skills .claude/skills`).
4. 기존 실폴더가 없어진 상태에서 Step 3 심볼릭 링크 연결로 진행한다.

---

## 3. 심볼릭 링크 생성 및 Git 등록 (필수)

기존 폴더가 비워졌으면 심볼릭 링크를 생성합니다.

```bash
node ../dev-automation/scripts/link-project.mjs --project .
```

### ① 링크 대상 및 스킬 노출 확인
두 경로 모두 정확히 **`../../dev-automation/skills`**를 가리키며 중앙 스킬이 조회되는지 확인합니다:

```bash
ls -la .agents/skills .claude/skills
ls -L .agents/skills
```

### ② 대상 프로젝트 Git에 심볼릭 링크 등록 (필수)
**`.agents/skills`와 `.claude/skills` 심볼릭 링크는 대상 프로젝트의 Git 저장소에 반드시 등록(추적)해 두어야 합니다.**

1. **`.gitignore` 점검**: 대상 프로젝트의 `.gitignore`(또는 `.agents/.gitignore`)에 `.agents/skills`나 `.claude/skills`가 무시 처리되어 있다면 해당 항목을 제거하거나 예외(`!.agents/skills`, `!.claude/skills`) 처리한다.
2. **Git 스테이징 및 symlink 모드(`120000`) 확인**:
   ```bash
   git add .agents/skills .claude/skills
   git ls-files -s .agents/skills .claude/skills
   ```
   출력의 파일 모드가 **`120000`**(Git symbolic link)으로 표시되어야 정상입니다.
3. 커밋·푸시는 사용자 승인 후 대상 프로젝트 브랜치에 반영한다.

> **팀/신규 환경 온보딩 전제**: 이 프로젝트를 새로 클론하는 환경에서는 대상 프로젝트와 같은 부모 디렉터리에 `dev-automation` 저장소도 함께 클론(`git clone ... ../dev-automation`)해 두기만 하면, 별도 스크립트 실행 없이 Git에 등록된 심볼릭 링크가 즉시 동작합니다.

---

## 4. 대상 프로젝트 `AGENTS.md` / `CLAUDE.md` 라우팅 반영

1. [templates/PROJECT_AGENTS_ROUTING.md](../templates/PROJECT_AGENTS_ROUTING.md)의 `## R&D Skill Governance & Stage Routing` 섹션을 대상 프로젝트 루트 `AGENTS.md`에 반영합니다.
2. **기존 프로젝트 규칙 보존**: 대상 프로젝트 `AGENTS.md`에 이미 적혀 있는 프로젝트 고유 아키텍처, 보안, 앱별 경계, 빌드/배포 규칙은 지우지 말고 유지합니다.
3. **Stage 2 스택 스킬 이름 동기화**: 라우팅 표의 Stage 2 행에 이 프로젝트에서 실제로 사용할 스택 스킬명(Step 2에서 중앙으로 이관한 스킬 포함)을 정확히 기입합니다.
4. **형제 폴더 전제 명시**: 대상 프로젝트 `AGENTS.md` 또는 `README.md`에 `../dev-automation` 형제 디렉터리 클론 전제를 한 줄로 안내해 두면 신규 환경 세팅 실수를 막을 수 있습니다.
5. **`CLAUDE.md` 처리**: 대상 프로젝트에 `CLAUDE.md`가 이미 있으면 중복 복붙으로 Drift를 만들지 말고, `AGENTS.md`를 따르도록 짧은 포인터를 두거나 최소한으로 동기화합니다.

---

## 5. CLI-Operable 검증 명령 명시 (권장)

Stage 2(`impl-autonomous-slice`), Stage 3 골든셋 A(`eval-release-pr-gate`), 그리고 향후 Phase 2 Go 하네스(`rd-project.yaml`, D-10)가 GUI 수동 조작에 묶이지 않고 검증할 수 있도록, 대상 프로젝트 루트 `AGENTS.md`에 아래 결정론적 CLI 명령이 명시되어 있는지 점검합니다:
- `typecheck` / `lint` / 정책 체크 명령
- 단위·비즈니스 TDD/TC 실행 명령
- 빌드 검증(`build` 또는 dry-run) 및 스모크 E2E 실행 명령

---

## 6. 최종 검증 체크리스트

세팅을 마치기 전 아래 항목을 모두 확인합니다:
- [ ] `dev-automation`과 대상 프로젝트가 **같은 부모 폴더 레벨(형제 디렉터리)**에 배치되어 있는가?
- [ ] 기존 `.agents/skills`·`.claude/skills` 실폴더의 스킬이 모두 분류·이관(또는 폐기)되고 실폴더가 완전히 비워졌는가?
- [ ] `.agents/skills`와 `.claude/skills`가 정확히 `../../dev-automation/skills` 심볼릭 링크를 가리키며 `ls -L .agents/skills`로 전체 중앙 스킬이 조회되는가?
- [ ] `.gitignore`에 막히지 않고 `git ls-files -s .agents/skills .claude/skills`에서 모드 **`120000`**(Git symlink)으로 추적 등록되었는가?
- [ ] 중앙 저장소(`../dev-automation`)로 이관/추가한 스킬이 있다면 `node scripts/validate-skills.mjs`가 오류 0건으로 통과하는가?
- [ ] 대상 프로젝트 루트 `AGENTS.md`에 Stage 1~4 주력 스킬 라우팅 표와 핵심 금지 원칙(자가 판정 금지, 전역 중단 조건, Fan-in 대기, Reward Hacking 금지)이 반영되었는가?
- [ ] Git commit / push / PR은 사용자가 명시적으로 승인한 범위에서만 수행했는가?

---

## 7. 외부 AI 스레드용 바로 실행 프롬프트 (복사용)

다른 프로젝트 스레드에서 이 가이드를 참조해 세팅을 진행할 때 아래 프롬프트를 그대로 붙여넣어 사용할 수 있습니다.

```text
../dev-automation/docs/PROJECT_SETUP_GUIDE.md 가이드를 읽고,
현재 프로젝트에 중앙 R&D 스킬 저장소(../dev-automation)를 세팅해줘.

필수 준수 사항:
- dev-automation과 현재 프로젝트는 항상 같은 부모 폴더 레벨(형제 디렉터리)에 둔다.
- 기존 `.agents/skills`, `.claude/skills`에 스킬이 있으면 별도 이관(중앙 이관 / 프로젝트 문서화 / 폐기) 후 실폴더를 완전히 비워야 심볼릭 링크를 연결할 수 있다.
- 연결한 심볼릭 링크(`../../dev-automation/skills`)는 `.gitignore`에 넣지 않고 반드시 현재 프로젝트 Git에 등록(`120000` symlink)한다.

진행 순서:
1. 먼저 형제 디렉터리 배치 확인, `node ../dev-automation/scripts/link-project.mjs --project . --dry-run`,
   현재 프로젝트의 `.agents/skills`, `.claude/skills`, `.gitignore`, 루트 `AGENTS.md`/`CLAUDE.md` 현황을 파악해서 보고해줘.
2. 기존 스킬이 있다면 중앙 이관 / 프로젝트 문서화 / 폐기 분류안을 제안하고 내 승인을 받아줘. (승인 전 삭제·이관 금지)
3. 내 승인 후 기존 스킬 이관 → 기존 실폴더 비우기 → 심볼릭 링크 연결 → Git 스테이징(`120000` 확인) → `AGENTS.md` 라우팅 반영 → 최종 체크리스트 검증까지 완료해줘.
```
