# Work History & Machine Handoff (작업 히스토리 및 이어하기 가이드)

> **목적**: 다른 머신 또는 새 AI 스레드에서 이 저장소(`dev-automation`) 작업을 즉시 이어갈 수 있도록, 현재까지의 작업 내역·검증 상태·남은 작업(Next Actions)을 시간순으로 기록합니다.
> 아키텍처 확정 결정(D-01 ~ D-13)은 [docs/DECISION_HISTORY.md](./DECISION_HISTORY.md)를 우선 확인합니다.

---

## 1. 작업 타임라인

### Phase A — 중앙 스킬 저장소 초기 구축 (2026-11-21 ~ 2026-11-22)
* `7164e8c` — 중앙 `rd-skills` 저장소 초기화 (`ARCHITECTURE.md`, `SKILL_AUTHORING_GUIDE.md`, `validate-skills.mjs`, `link-project.mjs`, 코어 게이트 스킬 4종: `gate-blueprint-check`, `eval-release-pr-gate`, `eval-senior-quality-gate`, `ops-remediation-director`)
* `5c0a0e0` — 불필요한 벤더별 `agents/openai.yaml`·`grok.yaml` 제거 (`SKILL.md` 단독 원칙, D-03)
* `133b168`, `1a91c5e` — Go 하네스·`AgentExecutor`·Telegram 프로젝트별 채팅·CLI-Operable 아키텍처 문서화 및 의사결정 히스토리 D-01 ~ D-11 기록

### Phase B — Stage 1~4 코어 10종 완성 + 전 스킬 일원화·이관 (2026-11-22)
* `ea43fc9` — Stage 1~4 코어 공식 스킬 6종 추가 (`design-system-arch`, `design-feature-spec`, `design-ui-ux`, `design-data-model`, `impl-autonomous-slice`, `ops-skill-evolution`) → 총 코어 10종 `official` 완비 + [PROJECT_AGENTS_ROUTING.md](../templates/PROJECT_AGENTS_ROUTING.md) 및 [PROJECT_SETUP_GUIDE.md](./PROJECT_SETUP_GUIDE.md) 작성
* `a24490a` — **D-12** 확정 및 반영:
  * `dev-automation`과 모든 대상 프로젝트를 **형제 디렉터리**로 고정 (`scripts/link-project.mjs`에 배치 검증 가드 추가)
  * 대상 프로젝트의 `.agents/skills`·`.claude/skills` 심볼릭 링크(`../../dev-automation/skills`)를 **Git(`120000`)에 직접 등록**
  * 공용·스택·비개발(`general`)·프로젝트 전용(`project:<repo>`) 스킬까지 모두 `skills/` 단일 플랫 구조로 일원화
  * `app-factory` 기존 스킬 9종을 `status: draft`로 규격화 이관 (`beta-feedback-triage`, `humanizer`, `product-ux-composition`, `rn-foundation-standard`, `rn-foundation-architecture-review`, `rn-foundation-quality-review`, `supabase`, `supabase-postgres-best-practices`, `tanstack-query-data-fetching`; 깨진 링크였던 `humanize-korean` 제외)

### Phase C — Go 얇은 워크플로우 하네스 MVP 구현 (2026-11-25)
* **D-13** 확정 및 `AGENTS.md` Meta Reward Hacking 방지 규칙(6번) 추가:
  * Go 하네스를 `dev-automation/harness/`에 단일 레포 통합, 런타임 상태·SQLite DB·토큰은 레포 바깥 `~/.dev-automation/`에 격리
  * 기본 역할 매핑(독립 OS 프로세스/세션 분리): Stage 1 기획·Gate 0=`gemini`, Stage 2 구현=`claude`, Stage 3-A 골든셋 A=No-AI Runner+`claude`(신규 프로세스), Stage 3-B 골든셋 B=`gemini`(Read-Only), Stage 4 진단·스킬 교정=`gemini`
  * 내부 테스트용 로컬 실행기(`LocalCLIGateway`) + 외부 창구용 Telegram Long-Polling(`TelegramGateway`, `1 Chat = 1 Project`, `allowed_user_ids` 화이트리스트) 구현
* **구현 완료 모듈 (`harness/`, Go 1.26 / Pure Go SQLite)**:
  * `internal/config`: `~/.dev-automation/config.yaml`(`0600`) 및 대상 프로젝트 `rd-project.yaml` 로더
  * `internal/store`: SQLite WAL (`modernc.org/sqlite`), 원자적 상태 전이, `chat_bindings`(1:1), `repo_lock_key` 큐, 데몬 재시작 고아 Run 복구(`RecoverOrphanedRuns`)
  * `internal/runner`: `Setpgid` 프로세스 그룹 킬 지원 No-AI 프로젝트 CLI 실행기(`RunChecks`, 미설정 항목 `OPEN_UNVERIFIED`) + D-12 형제 심볼릭 링크 원자적 온보딩(`EnsureSkillLinks`)
  * `internal/executor`: 단일 `AgentExecutor` 인터페이스, `ClaudeAdapter`(`claude -p --output-format json --json-schema`), `GeminiAdapter`(`gemini -p -o json`), `MockAdapter`, `SKILL.md` 강제 프롬프트 주입, Read-Only `git status` 불변 2중 검증(TC-05), `skills/` 쓰기 경계 가드(TC-06)
  * `internal/core`: Stage 1~4 상태 머신, Gate 0 승인 잠금(TC-01), `errgroup` 병렬 골든셋 A/B Fan-out → Fan-in(TC-02), `CRITICAL >= 1` 과락제(TC-03), Type S **선 스킬 교정 승인 잠금**(TC-04), 최대 재시도 Circuit Breaker
  * `internal/gateway`: `LocalCLIGateway` + `TelegramGateway`(Long-Polling `getUpdates`, `allowed_user_ids` 화이트리스트 TC-08, 인라인 승인 버튼, 4000자 분할 + `sendDocument` 첨부, 429 `retry_after` 백오프)
  * `cmd/rd-harness`: `init-config`, `init-project`, `chat`, `send`, `serve` 서브커맨드 + `--mock` 오프라인 테스트 모드

---

## 2. 현재 검증 완료 범위 vs 미관찰 범위 (`OPEN / UNVERIFIED`)

### 검증 완료 (2026-11-25 기준)
* `cd harness && go vet ./... && go test -v -race ./...` — 6개 패키지 전체 단위/통합 테스트(TC-01~TC-09 포함) 레이스 0건 통과
* 컴파일 바이너리 E2E 스모크 (`rd-harness send --mock`) — 실제 형제 프로젝트 `app-factory` 바인딩 → `/new` → 기획 → `/gate0` → `/approve` → Stage 2 → Stage 3 병렬 A/B → `READY_FOR_RELEASE` 완주 확인
* `node scripts/validate-skills.mjs` — 총 19개 스킬(`official` 10 + `draft` 9) 오류 0건 통과 (`humanizer` 625줄 경고 1건)

### 미관찰 런타임 범위 (`OPEN / UNVERIFIED` — 다음 머신에서 실행 필요)
1. **실제 `claude` / `gemini` CLI 실전 라운드트립**: 현재까지는 `MockAdapter`와 CLI `--help` 플래그 검증 완료 상태. 실제 API 호출 시 `claude`/`gemini` JSON 응답 포맷·세션 `--resume` 동작을 1바퀴 실측 검증해야 함.
2. **실제 Telegram Bot 토큰 연동**: `httptest` 목 서버로 Long-Polling/인라인 버튼/첨부/화이트리스트 검증 완료. 실제 BotFather 토큰을 넣어 실전 채팅방 송수신 검증 필요.

---

## 3. 다른 머신에서 바로 이어하기 체크리스트 (Next Actions)

### ① 환경 준비
```bash
# 1. 형제 디렉터리 구조 유지 (예: ~/dev/src/dev-automation, ~/dev/src/app-factory)
git pull origin main

# 2. 필수 툴 확인 (Go, Node, Claude CLI, Gemini CLI 로그인 상태)
go version && node -v && claude --version && gemini --version

# 3. 테스트 및 바이너리 빌드
cd harness && go vet ./... && go test -race ./... && go build -o ~/.dev-automation/bin/rd-harness ./cmd/rd-harness
```

### ② 설정 생성 및 오프라인 1차 확인 (`--mock`)
```bash
~/.dev-automation/bin/rd-harness init-config
~/.dev-automation/bin/rd-harness chat --mock
```

### ③ 실전 CLI 라운드트립 검증 (1순위)
1. 대상 형제 프로젝트(예: `../app-factory`) 루트에 `rd-project.yaml` 작성(`static_check`, `test`, `build_check` 실제 명령 기입 — `rd-harness init-project --project ../app-factory`로 초안 생성 가능).
2. `--mock` 없이 `~/.dev-automation/bin/rd-harness chat`으로 작은 테스트 과제 1바퀴 실행하며 실제 `gemini`/`claude` CLI 플래그·JSON 스키마 파싱·`--resume` 세션 유지 실측 검증 및 어댑터 미세 조정.

### ④ Telegram 실전 데몬 연결 (2순위)
1. `~/.dev-automation/config.yaml`에 `telegram.bot_token`과 본인 `telegram.allowed_user_ids` 입력.
2. `~/.dev-automation/bin/rd-harness serve` 실행 후 프로젝트 전용 텔레그램 채팅방에서 `/bind <project>` → `/new <title>` → `/gate0` → 인라인 버튼 승인 흐름 실전 검증.

### ⑤ 후속 백로그 (필요 시)
* `CodexAdapter` 추가 (설정 1줄로 3벤더 스위칭 필요 시)
* `app-factory`에서 이관한 `draft` 9종 실전 사용 후 `official` 승격 또는 폐기 (`humanizer` 승격 시 패턴 목록을 `references/`로 분리)
* macOS `launchd` 백그라운드 데몬 등록 스크립트 (상시 실행 안정화 이후)
