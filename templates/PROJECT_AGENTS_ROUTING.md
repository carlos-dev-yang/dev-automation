# 프로젝트 AGENTS.md용 R&D 스킬 라우팅 템플릿

아래 섹션을 개별 프로젝트의 루트 `AGENTS.md`(및 필요 시 `CLAUDE.md`)에 복사해 사용하면, Gemini(Antigravity)·Codex·Claude Code 등 모든 AI가 작업 단계별로 중앙 스킬(`rd-skills`)을 주력으로 호출합니다.

---

## R&D Skill Governance & Stage Routing

이 프로젝트는 중앙 스킬 저장소(`rd-skills`)의 스킬을 `.agents/skills` 및 `.claude/skills` 심볼릭 링크로 공유합니다. 모든 AI 에이전트는 임의로 작업 방식을 발명하지 않고, 아래 단계별 주력 스킬을 먼저 로드해 따릅니다.

| 작업 단계 | 수행 조건 | 필수 주력 스킬 |
| :--- | :--- | :--- |
| **Stage 1: 사전 설계·청사진** | 신규 기능·구조·UI·데이터 설계 논의 및 개발 착수 전 최종 점검 | 도메인 설계 스킬(`design-*`) + **`gate-blueprint-check`** (Gate 0 미통과 시 구현 착수 금지) |
| **Stage 2: 자율 구현** | Gate 0에서 확정된 로드맵·스펙에 따라 실제 코드 구현 | 스택/도메인 구현 스킬(예: `rn-foundation-standard`, `product-ux-composition`, `impl-*`, `supabase*`) |
| **Stage 3: 병렬 이중 골든셋** | 구현 완료 후 PR/출시 가능성 및 코드 품질 검증 | **구현 AI와 분리된 독립 리뷰어 2명이 동시 병렬 실행:**<br>1. **`eval-release-pr-gate`** (골든셋 A: 100% Hard Pass/Fail)<br>2. **`eval-senior-quality-gate`** (골든셋 B: 심각도 과락제 — `CRITICAL >= 1` 시 FAIL) |
| **Stage 4: 통합 교정 (선 스킬 → 후 작업)** | 골든셋 A·B **모두 검토가 완료된 뒤** 하나라도 FAIL 발생 시 | **`ops-remediation-director`** 실행 → A·B 통합 근본 원인 분석 및 단일 종합 교정 지시서 생성.<br>**필수 순서**: 스킬/청사진 결함이 있으면 **중앙 `rd-skills` 스킬 교정(PR/승인)을 먼저 완료한 뒤**, 교정된 스킬 기준으로 작업 코드를 수정한다. |

### 핵심 금지 원칙
* 구현 AI가 스스로 골든셋 A 또는 B를 최종 통과 판정하지 않는다.
* 골든셋 A·B 중 하나만 먼저 끝났다고 성급히 부분 수정을 시작하지 않고, **두 검토를 모두 완료한 뒤 한 번에 종합 교정 지시서로 수정**한다.
* 골든셋 실패 시 테스트만 통과시키기 위한 하드코딩·예외 무시·목(mock) 우회(Reward Hacking)를 금지한다.
