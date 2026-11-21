# rd-skills Agent Instructions

이 저장소는 여러 개발 프로젝트가 심볼릭 링크로 공유하는 **중앙 AI 스킬 저장소**입니다. 스킬을 추가·수정·승격할 때는 아래 규칙을 반드시 준수합니다.

## 기본 규칙

1. **표준 단일 플랫 구조 유지**
   - 모든 스킬은 `skills/<skill-name>/SKILL.md`에 배치합니다.
   - 수동 인덱스 파일(`skills-index.yaml`, `skill-index.md`)이나 물리적 `drafts/` 폴더를 만들지 않습니다. 모든 탐색 메타데이터와 `draft`/`official` 상태는 각 `SKILL.md`의 YAML frontmatter가 단일 원본(SSoT)입니다.
2. **작성 및 개정 규범 준수**
   - 스킬을 생성하거나 교정하기 전에 [SKILL_AUTHORING_GUIDE.md](./SKILL_AUTHORING_GUIDE.md)를 먼저 읽습니다.
   - 기존 스킬을 수정할 때 맨 아래에 예외 조항을 덧붙이지 않고, 기존 문제점과 신규 규칙을 통합해 **클린 재작성(Clean Rewrite)**합니다.
3. **Draft 및 승인 경계**
   - AI가 제안하는 신규 실험 스킬은 `metadata.status: draft`와 `description`의 `[DRAFT]` 프리픽스로 등록합니다.
   - 기존 `official` 스킬의 개정은 독립 브랜치/PR로 제안하며, 사람의 검토 없이 무단으로 기준을 완화하거나 머지하지 않습니다.
4. **변경 후 필수 검증**
   - 스킬을 추가하거나 수정한 뒤에는 반드시 `node scripts/validate-skills.mjs`를 실행해 frontmatter·네이밍·메타데이터·상대 링크에 오류가 없는지 확인합니다.
5. **원격 작업 경계**
   - Git 원격 등록, push, PR 생성 및 머지는 사용자가 명시적으로 요청하거나 승인한 범위에서만 수행합니다.
