# `rd-harness` (Go 얇은 워크플로우 하네스 + Telegram Gateway)

`dev-automation`의 Stage 1~4 스킬 계약을 코드(SQLite 상태 머신 + OS 프로세스 격리)로 강제하고, **로컬 테스트 실행기(`LocalCLIGateway`)**와 **Telegram 프로젝트별 채팅방(`1 Chat = 1 Project`, Long-Polling)**을 단일 Go 바이너리로 제공합니다.

> 모든 런타임 상태(SQLite DB, 실행 산출물 `runs/`, 텔레그램 봇 토큰)는 레포 바깥 **`~/.dev-automation/`**에 저장됩니다.

## 빌드 및 테스트

```bash
cd harness
go vet ./...
go test -race ./...
go build -o ~/.dev-automation/bin/rd-harness ./cmd/rd-harness
```

## 빠른 시작

### 1. 런타임 설정 생성 (`~/.dev-automation/config.yaml`, `0600`)
```bash
rd-harness init-config
```
기본 역할 매핑 (D-13, `config.yaml`에서 1줄 수정 가능):
* Stage 1 기획·설계 & Gate 0: `gemini` (`plan` 읽기 전용)
* Stage 2 자율 구현: `claude` (`acceptEdits`)
* Stage 3-A 골든셋 A: No-AI `rd-project.yaml` CLI 선행 실행 + `claude` (신규 독립 OS 프로세스, `plan`)
* Stage 3-B 골든셋 B: `gemini` (`plan`, 실행 전후 `git status` 불변 2중 검증)
* Stage 4 통합 교정·스킬 진화: `gemini` (`ops-skill-evolution`은 `skills/` 외 수정 시 즉시 차단)

### 2. 대상 프로젝트 온보딩 (형제 디렉터리 검증 + 심볼릭 링크 + `rd-project.yaml`)
```bash
rd-harness init-project --project ../app-factory --dry-run
rd-harness init-project --project ../app-factory
```

### 3. 내부 로컬 테스트 실행기 (`--mock`으로 오프라인 전체 루프 검증)
```bash
rd-harness chat --mock
# > /bind app-factory
# > /new Sample feature
# > 요구사항 입력...
# > /gate0
# > /approve
# > /status
```
실제 `claude`/`gemini` CLI로 로컬 실행하려면 `--mock`을 빼고 `rd-harness chat`을 실행합니다.

### 4. Telegram 데몬 실행 (`1 Chat = 1 Project`, Long-Polling)
`~/.dev-automation/config.yaml`에 `telegram.bot_token`(또는 `TELEGRAM_BOT_TOKEN` 환경변수)과 `telegram.allowed_user_ids` 화이트리스트를 설정한 뒤 실행합니다:
```bash
rd-harness serve
```
각 프로젝트 전용 텔레그램 채팅방에서 `/bind <project-name>` → `/new <title>` → 기획 대화 → `/gate0` → 인라인 **Approve & Build** 버튼으로 Stage 1~4를 제어합니다.
