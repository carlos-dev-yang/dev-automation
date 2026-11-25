# Architecture Decision History (의사결정 히스토리)

> **목적**: 이 문서는 2026-11-15 ~ 2026-11-25 논의 과정에서 **어떤 대안들을 검토했고, 왜 현재 구조로 확정했는지**를 기록합니다.
> 이 저장소에 참여하는 모든 AI 에이전트(Gemini, Claude, Codex, Grok 등)는 아래 **확정된 결정(Settled Decisions)**을 존중해야 하며, 사용자가 명시적으로 재검토를 요청하지 않는 한 이미 기각된 대안을 반복 제안하지 않습니다.

---

## 의사결정 요약표

| ID | 주제 | 기각/폐기된 대안 | 최종 확정 결정 |
| :--- | :--- | :--- | :--- |
| **D-01** | 스킬 저장소 위치·배포 | 프로젝트 내부 복사·개별 관리 | 독립 중앙 레포(`dev-automation`) + 프로젝트별 단일 심볼릭 링크 |
| **D-02** | 폴더 구조·인덱싱 | 물리 계층 폴더 분리 + 수동 `skills-index.yaml` | 단일 플랫 `skills/<name>/SKILL.md` + 표준 YAML Frontmatter가 유일한 인덱스 |
| **D-03** | 벤더별 부가 YAML | 모든 스킬에 `agents/openai.yaml`, `grok.yaml` 생성 | 기본 `SKILL.md` 단독 유지 (특수 Codex 정책 필요 시에만 예외 추가) |
| **D-04** | Draft 운영·비대화 방지 | 물리 `drafts/` 폴더 분리, 규칙 단순 덧붙이기(Append) | 단일 `skills/` 내 `metadata.status: draft` (신규) + Git Draft PR & 클린 재작성 (기존) |
| **D-05** | 골든셋 실행 구조 | 단일 AI 자가 평가, 골든셋 A → B 순차 실행 | **3자 완전 분리** (`AI #1 ≠ #2 ≠ #3`) + **A·B 동시 병렬 실행 → 모두 완료 후 Fan-in** |
| **D-06** | 골든셋 B 판정 기준 | 100점 만점 중 80점 이상 통과 점수제 | **심각도 기반 과락제** (`CRITICAL >= 1` 시 FAIL, `MEDIUM`/`LOW`는 Advice) |
| **D-07** | 실패 교정 순서·해킹 방지 | 실패 로그만 전달해 코드부터 자동 재시도 | 통합 `ops-remediation-director` 지시서 발급 + **스킬 선교정 → 작업 후교정** 강제 |
| **D-08** | 전사 공용 디자인 시스템 | 앱별 자유도 영구 고정 vs 지금 즉시 공용 킷 구축 | 장기 목표는 공용 디자인 시스템(중간점) 도입, **현시점은 앱 출시 후로 보류(Deferred ADR)** |
| **D-09** | 하네스 엔진·AI 통합·격리 | Node.js 엔진, LangChain/LangGraph 선도입, Docker 격리 | **Go 단일 바이너리** + 단일 `AgentExecutor` 인터페이스 + 레포 락/프로세스/파일 경량 격리 |
| **D-10** | 채팅 구조·프로젝트 제어 | 단일 통합 채팅, 텔레그램 토픽, GUI 수동 검증 의존 | **수신부/처리부 분리** + 텔레그램 **프로젝트별 채팅방** + **CLI-Operable 프로젝트 계약** |
| **D-11** | 구축 우선순위 | 자동화 시스템부터 선구축 | **스킬 단단하게 적용·검증(1순위)** → Go 하네스 및 텔레그램 자동화(2순위) |
| **D-12** | 스킬 일원화·형제 배치·Git 링크 | 프로젝트별 별도 로컬 스킬 폴더, 비개발 스킬 물리 분리, 심볼릭 링크 `.gitignore` | **전 스킬(`core`/`stack:*`/`general`/`project:<repo>`) 단일 일원화** + **형제 디렉터리 고정(`../../dev-automation/skills`)** + **대상 프로젝트 Git(`120000`) 등록** |
| **D-13** | Go 하네스 통합·역할 배치·Telegram | 별도 하네스 레포 분리, Webhook 서버, 벤더 강제 3분리만 허용 | **`dev-automation/harness/` 단일 레포 통합**(런타임은 `~/.dev-automation/` 격리) + **프로세스/세션 완전 분리 기반 `gemini`/`claude` 역할 배치** + **로컬 실행기 + Telegram Long-Polling** |

---

## 상세 의사결정 기록

### D-01. 독립 중앙 스킬 레포 + 심볼릭 링크 배포
* **초기 출발점**: `app-automation` 모노레포 내부 `.agents/skills/`를 기준으로 멀티 AI(Gemini, Claude, Codex)가 스킬을 공유하도록 정비하려 했음.
* **전환 이유**: 향후 `app-automation` 외 다른 R&D 프로젝트에서도 동일한 R&D 프로세스와 스킬셋을 써야 하며, 관리 주체가 1명(사용자)이므로 스킬은 별도의 독립 프로젝트로 분리하는 것이 자연스러움.
* **최종 결정**: `/Users/carlos-yang/dev/rd-skills` (원격: `carlos-dev-yang/dev-automation.git`)를 단일 원본(SSoT)으로 두고, 개별 프로젝트는 `.agents/skills`와 `.claude/skills`를 심볼릭 링크로 연결해 머지 즉시 전 프로젝트에 반영.

### D-02. 단일 플랫 폴더 + 수동 인덱스(`skills-index.yaml`) 폐기
* **검토했던 대안**:
  1. 스킬을 `core/`, `stack/`, `app/` 물리 폴더로 계층 분리
  2. 물리 폴더는 플랫하게 두되 `skills-index.yaml` 또는 `skill-index.md`를 별도로 두고 다중 태그 관리
* **기각 이유**:
  * 물리 폴더 분리는 여러 성격이 겹치는 스킬(예: Core이면서 Golden Set에 쓰이는 스킬) 관리가 경직됨.
  * Antigravity(Gemini), Claude Code, Codex, Grok 등 **모든 주요 AI가 이미 `.agents/skills/*/SKILL.md` 상단의 YAML frontmatter를 기본 스캔해 자체 인덱싱**함. 수동 인덱스 파일은 AI 오픈 스탠다드 역행 + 이중 관리(Drift) + 토큰 중복을 유발함.
* **최종 결정**: 수동 인덱스 파일을 두지 않고, 각 `SKILL.md`의 표준 frontmatter(`name`, `description`, `metadata`)를 유일한 인덱스로 사용. 향후 스킬이 수천 개 규모로 커지면 그때 frontmatter를 파싱해 자동 생성하는 검색 CLI만 추가.

### D-03. `agents/openai.yaml` · `agents/grok.yaml` 기본 미사용
* **검토했던 대안**: 기존 `app-automation` 일부 스킬의 관례대로 모든 스킬마다 `agents/openai.yaml`과 `agents/grok.yaml`을 함께 생성.
* **기각 이유**: Codex와 Grok 모두 `SKILL.md`의 표준 frontmatter만으로 100% 정상 탐색·호출됨. `openai.yaml`은 UI 표시 문구나 암묵적 자동 호출 차단(`allow_implicit_invocation: false`) 등 특수 정책이 필요할 때만 쓰는 선택 파일임.
* **최종 결정**: 기본은 `skills/<name>/SKILL.md` 단일 파일만 유지해 이중 관리를 없앰.

### D-04. `draft` 운영(물리 폴더 없음) 및 클린 재작성(Rewrite)
* **검토했던 대안**:
  1. `official/`과 `drafts/` 물리 폴더 분리 + 심볼릭 링크 동기화 스크립트
  2. 피드백이 생길 때마다 기존 `SKILL.md` 맨 아래에 예외 규칙 계속 덧붙이기(Append)
* **기각 이유**:
  * 물리적 `drafts/` 폴더 분리는 AI 표준 경로(`.agents/skills/<name>/SKILL.md`)와 어긋나 별도 링크 동기화 스크립트를 요구하고, 승격 시 폴더 이동 비용이 발생함.
  * 단순 덧붙이기는 컨텍스트 비대화와 규칙 간 충돌을 일으킴.
* **최종 결정**:
  * 물리 폴더는 단일 `skills/`만 유지.
  * AI가 새로 제안하는 실험 스킬은 `metadata.status: draft`와 `[DRAFT]` 프리픽스를 달아 실전에서 정식 스킬과 함께 시험한 뒤 유용하면 `official`로 승격(또는 삭제/revert).
  * 기존 정식 스킬 개정은 Git Draft PR을 스테이징으로 삼고, 덧붙이기 대신 **기존 문제점과 신규 룰을 통합한 클린 재작성(Clean Rewrite)**으로 갱신.

### D-05. 3자 완전 분리 + 골든셋 A·B 동시 병렬 실행 후 Fan-in
* **초기 모델**: 골든셋 A(출시 PR 테스트) 통과 후 골든셋 B(시니어 리뷰) 순차 실행.
* **전환 이유**:
  1. 구현한 AI가 스스로 평가하면 확증 편향으로 게이트가 무력화되므로 `구현 AI` ≠ `골든셋 A AI` ≠ `골든셋 B AI` 3자 완전 분리가 필수.
  2. A와 B는 독립적이므로 동시에 병렬 실행해야 대기 시간이 줄어듦.
  3. A 통과가 B 무결함을 보장하지 않으며, A가 깨졌을 때 B의 아키텍처 결함도 같이 잡아야 **한 번의 종합 수정으로 핑퐁(A 고치고 → B 터져서 재작업)을 방지**할 수 있음.
* **최종 결정**: A·B 동시 병렬 수행 → **둘 다 검토가 끝난 뒤(Fan-in)** 결과를 합산.

### D-06. 골든셋 B의 ‘80점 점수제’ 폐기 → ‘심각도 기반 과락제’
* **초기 모델**: 골든셋 B(또는 제품 골든셋)를 100점 만점 중 80점 이상이면 통과하는 점수제로 설계.
* **기각 이유**: 100개 항목 중 치명적 결함 1개가 존재해도 99점으로 통과해 버리는 심각한 맹점이 있으며, 실제로는 경미한 리뷰 의견이 있어도 출시 가능한 경우가 많음.
* **최종 결정**:
  * **골든셋 A (출시 PR)**: 하나라도 깨지면 무조건 차단하는 **100% Hard Pass/Fail**.
  * **골든셋 B (시니어 리뷰)**: 우선순위·심각도 기반 **과락제** — `CRITICAL`이 1건이라도 있으면 무조건 `FAIL`, `MEDIUM`/`LOW`는 Advice(권고)로 남기고 통과 허용.

### D-07. Reward Hacking 차단 및 ‘스킬 선교정 → 작업 후교정’ 순서 강제
* **문제 인식**: 골든셋 실패 시 구현 AI에게 결과만 던져 자동 재시도하게 두면, AI는 근본 원인을 고치지 않고 테스트만 통과하는 하드코딩 땜질(Reward Hacking)을 하기 쉬움.
* **최종 결정**:
  * 사람 시니어 리드의 역할을 대리하는 전용 교정 지시 스킬(`ops-remediation-director`)을 신설.
  * A·B 검토가 모두 끝난 뒤 두 리포트와 작업 diff/경로를 통합 분석해 **단일 종합 교정 지시서**를 발급.
  * **순서 강제**: 원인 분석 결과 스킬/청사진 결함(Type S)이 있으면 **작업 코드를 먼저 고치지 않고 반드시 스킬 교정(PR/승인)을 선행한 뒤**, 교정된 스킬을 기반으로 작업(코드)을 교정.

### D-08. 전사 공용 디자인 시스템 — 출시 후 도입 (Deferred ADR)
* **긴장 지점**: 기존 `app-automation`의 `docs/rn-foundation-ui/README.md`는 앱별 시각 자유도를 최우선으로 두지만, R&D 속도와 UI 일관성 측면에서는 전사 모노레포 공용 디자인 시스템이 유리함.
* **최종 결정**: 장기적으로는 앱별 자유도를 존중하되 전사 공용 디자인 시스템을 기준점으로 두는 **적절한 중간점**(필요 시 `rn-foundation` 개정)으로 진화. 단, **현시점에서는 선구축하지 않고 앱이 실제 출시된 이후 도입**하며, 그전까지는 현행 `rn-foundation-ui/README.md` 기준을 준수.

### D-09. Go 기반 경량 하네스 + 단일 `AgentExecutor` (LangChain/LangGraph 보류, 컨테이너 미사용)
* **검토했던 대안**:
  1. Node.js/TypeScript 기반 워크플로우 엔진
  2. 멀티 모델(Gemini/Claude/Codex) 전환을 위한 LangChain / LangGraph 초기 도입
  3. 프로젝트/작업 격리를 위한 Docker 컨테이너
* **기각 이유**:
  * Node.js는 상시 백그라운드 데몬으로 두기엔 메모리 사용량이 높고 성능 이점이 적음.
  * LangChain은 파일 편집·터미널을 쓰는 코딩 CLI(`claude`/`codex`/`gemini`) 호출에 과잉 추상화이며, 단순 인터페이스 통일이 목적이라면 얇은 자체 인터페이스가 훨씬 단순·안정적임. (LangGraph 역시 추후 필요 시 재검토로 보류)
  * Docker 컨테이너는 1인 운영에 과도하게 무겁고, macOS의 Xcode/iOS 시뮬레이터·로컬 Gradle 환경과 호환성이 나쁨.
* **최종 결정**:
  * 하네스 엔진은 **Go** (저메모리 상시 데몬, `errgroup` 네이티브 동시성, 단일 바이너리) + 필요 모듈만 Python 서브프로세스 연결.
  * AI 호출은 Go의 단일 **`AgentExecutor` 인터페이스**로 Gemini · Claude · Codex(CLI/API) 통합.
  * 격리는 **레포 락(순차 큐) + OS 프로세스 분리(골든셋 B `ReadOnly`) + 파일 기반 산출물(`runs/`)**로 경량 구현.

### D-10. 채팅 수신부/처리부 분리 + 텔레그램 프로젝트별 채팅 + CLI-Operable 프로젝트
* **검토했던 대안**:
  1. 단일 메인 채팅방에서 모든 프로젝트 제어
  2. 텔레그램 슈퍼그룹 1개 + Forum Topic(주제별) 운영
* **기각 이유**:
  * 단일 채팅은 컨텍스트 오염과 승인 상태 추적이 어려워, 프로젝트별 개별 채팅을 1급 객체로 먼저 두어야 나중에 상위 메인 오케스트레이터/대시보드에서 서브 프로젝트 작업물에 깔끔히 접근 가능.
  * 텔레그램에서는 토픽보다 **프로젝트별 개별 채팅방(`1 Chat = 1 Project`)**이 운영상 더 명확함.
* **최종 결정**:
  * 채팅 플랫폼 종속 **수신·송신부(Channel Gateway)**와 플랫폼 무관 **내부 처리부(Chat Core)**를 분리(Telegram 우선 → 추후 Slack/Web UI/CLI 확장).
  * 하네스와 채팅이 프로젝트에 접근해 조작·보고할 수 있도록 **앱/프로젝트 자체도 표준 CLI 계약(`rd-project.yaml`)으로 빌드·테스트·E2E·상태 조회가 가능하게(CLI-Operable)** 설계.

### D-11. 실행 우선순위: 스킬 안착 우선 → 하네스·채팅 후속
* **최종 결정**: 초반에는 복잡한 자동화 시스템을 먼저 짓지 않고, **`rd-skills`를 단단하게 정비해 실전 프로젝트에 먼저 붙이는 것(1순위)**이 우선. 스킬이 검증되고 안정화되면(또는 병행 준비를 거쳐) 그 스킬을 기본 전제로 깔고 도는 Go 하네스와 텔레그램 채팅 시스템(2순위)을 구축.

### D-12. 전 스킬 단일 일원화 + 형제 디렉터리 고정 + 심볼릭 링크 Git 등록
* **검토했던 대안**:
  1. 공용 스킬과 프로젝트 전용 스킬·비개발 스킬을 별도 폴더나 프로젝트 내부 `.agents/skills` 실폴더로 분리 관리
  2. `.agents/skills`, `.claude/skills` 심볼릭 링크를 `.gitignore`로 제외해 환경마다 수동 재연결
* **기각 이유**:
  * 프로젝트의 `.agents/skills`와 `.claude/skills`를 단일 심볼릭 링크로 묶는 구조에서는 프로젝트 내부 별도 스킬 폴더를 병행할 수 없고, 비개발/전용 스킬을 분리하면 폴더 관리와 AI 자동 탐색이 복잡해짐.
  * `dev-automation`과 모든 대상 프로젝트를 항상 같은 부모 폴더 레벨(형제 디렉터리)에 두면 상대 경로가 예외 없이 `../../dev-automation/skills`로 고정되므로 `.gitignore`로 뺄 이유가 없음.
* **최종 결정**:
  * 공용(`core`), 스택(`stack:*`), 비개발(`general`), 특정 프로젝트 전용(`project:<repo>`)까지 **모든 스킬을 `dev-automation/skills/` 단일 플랫 구조로 일원화**하고, 오동작 차단은 `description`(레포·사용 조건 명시)과 `metadata.scopes`로 제어.
  * 스킬 실행에 필요한 추가 환경·런타임 요건은 `SKILL.md` 사전 요건에 명시. 범용/비개발 자체 스크립트는 `skills/<name>/scripts/`에 동봉하고, 프로젝트 종속 스크립트는 해당 프로젝트에 두되 `SKILL.md`에서 CWD 기준 경로로 참조.
  * 모든 대상 프로젝트는 기존 `.agents/skills`·`.claude/skills` 실폴더를 비운 뒤 `../../dev-automation/skills` 심볼릭 링크를 생성해 **대상 프로젝트 Git(`120000`)에 직접 등록**(`scripts/link-project.mjs`에서 형제 배치 검증).

### D-13. `dev-automation/harness/` 통합 + 프로세스 분리 역할 배치 + 로컬 실행기/Telegram Long-Polling
* **검토했던 대안**:
  1. Go 하네스를 별도 저장소(`rd-harness`)로 분리
  2. Telegram Webhook HTTPS 서버 구성
  3. 3자 분리를 반드시 서로 다른 3개 벤더로만 제한
* **기각 이유**:
  * 스킬 출력 계약(`Gate 0`, `Golden Set A/B`, `Remediation Sequence`)과 하네스 파서·상태 머신은 한 셋이므로, 레포 분리 시 버전 드리프트와 온보딩 복잡도만 증가함. (대상 프로젝트는 `../../dev-automation/skills`만 링크하므로 `harness/` 노출 없음)
  * 1인 로컬 Mac 데몬 환경에서는 공인 IP/TLS가 필요한 Webhook보다 Long-Polling(`getUpdates`)이 단순·안전함.
  * D-05 3자 분리의 본질은 **구현 컨텍스트와 평가 컨텍스트의 완전 단절(독립 스레드·별도 OS 프로세스)**이며, 실행기만 독립 분리하면 동일 벤더 CLI를 쓰더라도 확증 편향을 차단할 수 있음(설정 1줄로 타 어댑터 확장 가능).
* **최종 결정**:
  * 하네스 소스는 `dev-automation/harness/`에 두고, 런타임 상태(SQLite DB, `runs/`, 시크릿 토큰)는 레포 바깥 `~/.dev-automation/`에 격리.
  * 기본 역할 매핑(독립 프로세스 분리 실행): Stage 1 기획·Gate 0 = `gemini`, Stage 2 구현 = `claude`, Stage 3-A 골든셋 A = No-AI Runner + `claude`(신규 독립 프로세스), Stage 3-B 골든셋 B = `gemini`(Read-Only), Stage 4 진단·스킬 교정 = `gemini`.
  * 내부 테스트 가능한 로컬 실행기(`LocalCLIGateway`)와 외부 운영 창구(`TelegramGateway` Long-Polling + `allowed_user_ids` 화이트리스트)를 동일 `Chat Core` 위에 배치.
  * Stage 4 스킬 교정 AI(`ops-skill-evolution`)는 `skills/` 경로만 수정 가능하며 `harness/` 무단 수정을 금지(Meta Reward Hacking 차단).
