# `rd-harness` (Go 얇은 워크플로우 하네스 + 외부 송수신 프로토콜 + 2계층 채팅·오케스트레이션 + Telegram 어댑터)

`dev-automation`의 Stage 1~4 스킬 계약을 코드(SQLite 상태 머신 + OS 프로세스 격리)로 강제하고, 채널 중립 **외부 송수신 프로토콜(`HTTP+JSON` + `SSE`)** 위에 **앞단 채팅 모델(`gpt-6-luna`) ↔ 중앙 오케스트레이터(`claude-haiku`) 2계층 구조**와 **Telegram 연동 모듈(`1 Chat = 1 Project`, Long-Polling)**을 결합합니다.

> 모든 런타임 상태(SQLite DB, 실행 산출물 `runs/`, 하네스 내부 인증 `auth/`, 텔레그램 봇 토큰)는 레포 바깥 **`~/.dev-automation/`**에 저장됩니다.

## 아키텍처 경계

```text
[외부 채널 어댑터]                  [외부 송수신 프로토콜 v1]
- Telegram Adapter (Long-Polling) ──┐
- Local CLI / curl / 테스트 스크립트 ──┼─► protocol.Transport (POST /v1/messages, GET /v1/events SSE, GET /v1/health)
- 향후 Slack / Web UI / Webhook   ──┘                │
                                                     ▼
                               [앞단 채팅 모듈: internal/chat]
                               ChatResponder (기본: codex / gpt-6-luna)
                               - 작업 비종속적: 요구사항 수신·전달 + 결과 채팅 응답 변환
                                                     │  HandoffRequest / OrchestrationResult
                                                     ▼
                               [중앙 오케스트레이터: internal/orchestrator]
                               Orchestrator (기본: claude / claude-haiku)
                               - 노드/프로젝트 & 스킬 카탈로그(SKILL.md frontmatter) 탐색
                               - 작업 분해·순차 실행 순서·우선순위·잔여 작업 계산·내부 지시
                                                     │
                                                     ▼
                               [하위 실행 계층: internal/core + runner + executor]
                               Stage 1~4 워크플로우 엔진 / No-AI CLI / AI Provider Adapters
```

## 빌드 및 테스트

```bash
cd harness
go vet ./...
go test -race ./...
go build -o ~/.dev-automation/bin/rd-harness ./cmd/rd-harness
```

## 빠른 시작

### 1. 런타임 설정 및 로컬 `.env` 시크릿 생성 (`~/.dev-automation/`, `0600`)
```bash
rd-harness init-config
```
- `~/.dev-automation/config.yaml` (`0600`)과 `~/.dev-automation/.env` (`0600`, 샘플은 [harness/.env.example](./.env.example))가 레포 바깥에 자동 생성됩니다.
- 기본 대화·오케스트레이션 역할 매핑:
  - `conversation.chat_responder`: `adapter: codex`, `model: gpt-6-luna` (앞단 채팅 — ChatGPT 구독 CLI)
  - `conversation.orchestrator`: `adapter: anthropic-api`, `model: claude-haiku-5-5` (중앙 오케스트레이터 — Anthropic HTTP API + Prompt Caching)
- `~/.dev-automation/.env`에 `ANTHROPIC_API_KEY=sk-ant-...`를 입력하면 `rd-harness`가 시작 시 자동 로드합니다(프로세스 환경변수가 있으면 최우선 적용).

### 2. ChatGPT 구독 인증을 하네스 내부에 저장 (`~/.dev-automation/auth/codex`)
```bash
# 현재 저장된 인증 상태 확인 (하네스 내부 auth dir + 글로벌 ~/.codex)
rd-harness auth chatgpt --status

# ChatGPT 구독 OAuth 인증 요청 후 ~/.dev-automation/auth/codex 에 저장
rd-harness auth chatgpt --device-auth
```

### 3. 텔레그램 없이 외부 프로토콜 + 2계층 채팅 먼저 검증
```bash
# 터미널 1: 외부 송수신 프로토콜 서버만 먼저 실행 (연결 전 테스트는 --mock)
rd-harness serve --protocol-only --mock

# 터미널 2: 헬스체크 / 프로토콜 왕복 / 2계층 채팅 / 백로그·노드 조회
curl -s http://127.0.0.1:8787/v1/health
rd-harness send --endpoint http://127.0.0.1:8787 --channel http --chat-id test-1 "/ping"
rd-harness send --endpoint http://127.0.0.1:8787 --channel http --chat-id test-1 "/chat 우선순위대로 남은 작업 정리해줘"
rd-harness send --endpoint http://127.0.0.1:8787 --channel http --chat-id test-1 "/tasks"
rd-harness send --endpoint http://127.0.0.1:8787 --channel http --chat-id test-1 "/nodes"
```

### 4. Telegram 연동 모듈 부착 (어댑터 전용 시크릿 분리)
텔레그램 토큰은 코어 `.env`가 아닌 **어댑터 전용 파일** `~/.dev-automation/adapters/telegram.env` (`0600`, 샘플은 [harness/adapters/telegram.env.example](./adapters/telegram.env.example))에서 관리합니다.

```bash
# 어댑터 전용 시크릿 템플릿 생성 (~/.dev-automation/adapters/telegram.env)
rd-harness telegram --init

# 방식 A (통합 실행): 프로토콜 서버 + Telegram 어댑터를 한 프로세스로 실행
rd-harness serve

# 방식 B (분리 브릿지): 실행 중인 프로토콜 서버에 Telegram 어댑터만 외부 모듈로 부착
rd-harness serve --protocol-only
rd-harness telegram --endpoint http://127.0.0.1:8787
```

텔레그램 채팅방 점검 순서:
1. `/ping` — 텔레그램 ↔ 프로토콜 ↔ 하네스 왕복 확인 (No-AI)
2. `/chat <메시지>` — 앞단 채팅(`gpt-6-luna`) ↔ 중앙 오케스트레이터(`claude-haiku`) 2계층 대화 확인
3. `/tasks` / `/nodes` — 오케스트레이터 백로그(잔여 작업·우선순위) 및 컨텍스트/스킬 레지스트리 확인
4. `/bind <sibling-project>` → `/new <title>` → 기획 대화 → `/gate0` → 인라인 버튼 승인
