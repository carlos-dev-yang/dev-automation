# Work History & Machine Handoff (작업 히스토리 및 이어하기 가이드)

> **목적**: 다른 머신 또는 새 AI 스레드에서 이 저장소(`dev-automation`) 작업을 즉시 이어갈 수 있도록, 현재까지의 작업 내역·검증 상태·최근 논의 합의·남은 작업(Next Actions)을 시간순으로 기록합니다.
> 아키텍처 확정 결정(D-01 ~ D-14)은 [docs/DECISION_HISTORY.md](./DECISION_HISTORY.md)를 우선 확인합니다.

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
  * `app-factory` 기존 스킬 9종을 `status: draft`로 규격화 이관

### Phase C — Go 얇은 워크플로우 하네스 MVP 구현 (2026-11-25)
* `12ed437` — **D-13** 확정 및 `AGENTS.md` Meta Reward Hacking 방지 규칙(6번) 추가:
  * `harness/` Go 1.26 / Pure Go SQLite WAL 기반 Stage 1~4 상태 머신, `errgroup` 병렬 골든셋 A/B Fan-out → Fan-in, Read-Only 불변 가드, `skills/` 쓰기 경계 가드, `LocalCLIGateway` + `TelegramGateway` 구현

### Phase D — 외부 송수신 프로토콜 1급 분리 · 2계층 채팅/오케스트레이션 · 실전 모델 검증 (2026-11-25 ~ 2026-11-27)
* **D-14** 확정 및 구현:
  * **외부 송수신 표준 프로토콜 (`harness/internal/protocol`)**:
    * 채널 중립 `IngressMessage` / `EgressMessage` / `HealthStatus` 스키마 및 `Transport` 인터페이스
    * 인프로세스 `Broker`(비동기 이벤트 fan-out) + 네트워크 `Server`/`HTTPClient`(`GET /v1/health`, `POST /v1/messages`, `GET /v1/events` SSE, 선택적 Bearer 인증)
    * `TelegramGateway`와 `LocalCLIGateway`에서 `core.Engine` 직접 의존을 제거해 순수 `protocol.Transport` 어댑터로 분리 (채팅방별 순차 비동기 워커, `typing` 액션, `getMe` 검증, 독립 브릿지 `rd-harness telegram --endpoint ...` 지원)
  * **2계층 대화 · 오케스트레이션 모듈 분리**:
    * 앞단 채팅 모듈 (`harness/internal/chat`, `ChatResponder`): 기본 **`codex/gpt-6-luna`** — 작업 비종속적으로 사용자 의도 수신·정규화 전달(Intake, OpenAI strict JSON Schema) + 오케스트레이터 결과를 자연어 채팅 응답으로 변환(Presentation) 및 비동기 중계(`RelayResult`)
    * 중앙 오케스트레이터 모듈 (`harness/internal/orchestrator`, `Orchestrator`): 기본 **`anthropic-api/claude-haiku-5-5`** — `LocalContextRegistry`(SQLite 노드 + `skills/*/SKILL.md` frontmatter 자동 스캔), 작업 분해·순차 순서·우선순위(P1~P5)·잔여 작업 수 결정론적 재계산, `TaskDispatcher` 브릿지
    * Chat Core 커맨드 추가: `/ping`, `/chat <msg>`, `/tasks`, `/nodes`, `/ask <msg>`, `/reset`
  * **프로바이더 어댑터 및 인증/시크릿 격리**:
    * `CodexAdapter` (`harness/internal/executor/adapters.go`): `codex exec` + `--output-schema` + `CODEX_HOME` 자동 연결 + `ResolveCodexBinary`(ChatGPT 앱 번들 최신 바이너리 `0.171.0` 우선 선택으로 `gpt-6-luna` 카탈로그 호환 해결) + `rd-harness auth chatgpt [--device-auth|--status]`로 `~/.dev-automation/auth/codex`에 구독 인증 저장
    * `AnthropicAPIAdapter` (`harness/internal/executor/anthropic_api.go`): Anthropic HTTP Messages API 직접 호출, 시스템 블록 `cache_control: ephemeral` Prompt Caching 적용, `TokenUsage` 파싱, 추후 클라우드 WIF(OIDC) 전환용 `AnthropicAuthProvider` 인터페이스 분리
    * `GeminiAdapter` 실측 교정: `gemini 0.22.5`의 유효 플래그 `--approval-mode default` 수정, `GOOGLE_CLOUD_PROJECT` 전달, 구조화 에러 파싱, 세션 폴백
    * 시크릿 물리/로더 완전 분리 (`harness/internal/config/dotenv.go`):
      * 코어 전용: `~/.dev-automation/.env` (`0600`, 샘플 [harness/.env.example](../harness/.env.example)) — `ANTHROPIC_API_KEY` 등 코어 자격 증명만 로드
      * 어댑터 전용: `~/.dev-automation/adapters/telegram.env` (`0600`, 샘플 [harness/adapters/telegram.env.example](../harness/adapters/telegram.env.example)) — `LoadTelegramAdapterEnv`로 Telegram 어댑터만 로드 (`rd-harness telegram --init`)

---

## 2. 최근 아키텍처 논의 요약 (2026-11-26 ~ 2026-11-27)

1. **구독 CLI vs HTTP API 정책 및 비용 검토**:
   * Anthropic 소비자 구독 OAuth(`claude -p`)를 상시 봇/하네스에 연결하는 것은 2026년 ToS 위반 및 계정 정지 리스크가 높아 **Claude는 HTTP API 키(`claude-haiku-5-5`, 입력 \$0.10/1M·캐시 \$0.01/1M·출력 \$0.50/1M, 개인 2계층 채팅 월 ~\$1~2)**로 확정.
   * OpenAI는 우선 ChatGPT 구독 CLI(`codex/gpt-6-luna`)로 운영하면서 추후 비용/확장 필요 시 API 어댑터로 스위칭 가능하게 유지.
   * Anthropic Workload Identity Federation(WIF/OIDC)은 로컬 Mac에 공인 OIDC Issuer가 없어 실익이 낮으므로 클라우드/CI 배포 시점으로 보류(`AnthropicAuthProvider` 확장점만 확보).
2. **메인 AI 비서(1) → 중앙 오케스트레이터(1) → 프로젝트별 서브 스레드(N) 구조 합의**:
   * 텔레그램 봇 1개의 메인 채팅 창구(`ChatResponder`)에서 전 프로젝트 작업을 지시·보고받고, 중앙 `Orchestrator`가 하위 프로젝트들의 독립 서브 스레드(`WorkThread`/`runs`) 생성·라우팅·우선순위·잔여 작업을 총괄하며, 실제 작업은 각 서브 스레드의 격리 컨텍스트에서 실행.
3. **SQLite 단일 원본(SSoT) 기반 대화 기록 & 슬라이딩 컨텍스트 주입 합의**:
   * 현재 `chat_messages`(전체 대화 원문)와 `orchestrator_tasks`(백로그)는 아직 미영속 상태(백로그는 메모리 보유).
   * 다음 구현 단계에서 전체 대화 원문·백로그·스레드 요약을 SQLite에 영속 저장하고, HTTP API 호출 시 **앞단 채팅은 최근 N턴 + 압축 요약**, **오케스트레이터는 프롬프트 캐시 영역(규칙·스킬·프로젝트) + 활성 서브 스레드 요약 카드 + 백로그 + 최근 대화**만 슬라이싱 주입하기로 정리.

---

## 3. 현재 검증 완료 범위 vs 미관찰/남은 범위

### 검증 완료 (2026-11-27 기준)
* `cd harness && go vet ./... && go test -race ./...` — **9개 패키지 전체 통과** (`chat`, `config`, `core`, `executor`, `gateway`, `orchestrator`, `protocol`, `runner`, `store`, 레이스 0건)
* `node scripts/validate-skills.mjs` — 총 19개 스킬(`official` 10 + `draft` 9) 오류 0건 통과
* **실전 2계층 채팅·프로토콜 왕복 검증 (`--mock` 없이 실제 모델 호출)**:
  * `~/.dev-automation/auth/codex` (ChatGPT 구독 인증) + `~/.dev-automation/.env` (`ANTHROPIC_API_KEY`) + `rd-harness serve --protocol-only`로 실제 HTTP 통신 검증 완료:
    * `GET /v1/health` → `chat_responder: codex/gpt-6-luna`, `orchestrator: anthropic-api/claude-haiku-5-5`
    * 단순 인사 (`/chat 안녕, 반가워`) → `codex/gpt-6-luna` 단독 응답 (오케스트레이터 미호출)
    * 작업 요청 (`/chat ...`) → `codex/gpt-6-luna`(Intake) → `anthropic-api/claude-haiku-5-5`(노드/스킬 19종 탐색 + 작업 2건·우선순위 P2·잔여 2건 분해 + 질의 생성) → `codex/gpt-6-luna`(자연어 채팅 응답 변환) → `/tasks`·`/nodes` 정상 확인

### 미관찰 런타임 범위 (`OPEN / UNVERIFIED`)
1. **실제 Telegram Bot 토큰 실전 송수신**: `httptest` 기반 HTTP 프로토콜 + SSE 통합 테스트 완료. 실제 BotFather 토큰을 `~/.dev-automation/adapters/telegram.env`에 넣어 실전 채팅방 왕복 검증 필요.
2. **Stage 2~4 실제 `claude`/`gemini` 코딩 CLI 풀 루프**: 현재 Stage 1~4 파이프라인은 `MockAdapter`로 검증 완료. 앞단 채팅·오케스트레이터 영속화 이후 실전 프로젝트 1바퀴 검증 예정.

---

## 4. 다음 작업 체크리스트 (Next Actions)

### ① SQLite 대화 원문 · 오케스트레이터 백로그 영속화 + 슬라이딩 컨텍스트 주입 (1순위)
* `store.go`에 `chat_messages`(전체 사용자↔비서 대화 로그)와 `orchestrator_tasks`(스레드/스코프별 작업·우선순위·잔여 상태) 테이블 추가
* `AnthropicAPIAdapter`에 멀티턴 `messages` 슬라이딩 컨텍스트 주입 지원 추가
* `Orchestrator`가 등록된 하위 프로젝트(`projects`)와 활성 서브 스레드(`runs`)의 **요약 카드**를 컨텍스트로 받아 멀티턴 대화를 이어가도록 연결

### ② 메인 AI 비서 → 프로젝트별 서브 스레드(`WorkThread`/`runs`) 생성·라우팅 연결 (2순위)
* 형제 프로젝트 자동 스캔/등록 + `Orchestrator` 액션(`CREATE_THREAD`, `ROUTE_TO_THREAD`, `REORDER_BACKLOG`, `REPORT_STATUS`)을 실제 `store.runs`와 연결

### ③ Telegram 봇 실전 부착 검증 (3순위)
* `~/.dev-automation/adapters/telegram.env`에 `TELEGRAM_BOT_TOKEN`, `TELEGRAM_ALLOWED_USER_IDS` 입력 후 `rd-harness serve` 또는 `rd-harness telegram --endpoint http://127.0.0.1:8787`로 실전 텔레그램 송수신 점검

### ④ 후속 백로그 (앞단 환경 안착 후)
* 하네스 핵심 보장 기능 고도화: frontmatter 기반 동적 스킬 선택·강제 주입, 구조화 기준 문서(Living Blueprint), Gate 0 `Roadmap` 기반 Slice 단위 Stage 2 분할 루프
* `app-factory` 이관 `draft` 9종 실전 검증 후 `official` 승격 또는 폐기
* macOS `launchd` 백그라운드 데몬 등록 스크립트
