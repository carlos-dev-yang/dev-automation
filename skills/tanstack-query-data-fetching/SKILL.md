---
name: tanstack-query-data-fetching
description: >-
  [DRAFT][Stage 2][React/RN] Enforce TanStack Query for API/server data
  fetching, prevent unnecessary refetches and re-renders, and minimize useEffect
  usage across React and React Native apps. Use when implementing, modifying, or
  reviewing API calls, screen data loading, pagination, focus refetching,
  caching, or state/provider render cascades.
metadata:
  status: draft
  version: "1.0"
  stages: [stage-2]
  scopes: [stack:react, stack:rn]
  roles: [implementer]
---

# TanStack Query Data Fetching & Minimal `useEffect`

서버/API 데이터 조회는 `@tanstack/react-query`를 기본으로 사용하고, `useEffect`는 외부 시스템 동기화 외에는 사용하지 않는다. 각 화면이 지금 필요한 데이터만 조회하고 불필요한 재조회와 리렌더링을 일으키지 않도록 구현한다.

## 1. API·서버 데이터는 TanStack Query로 통일한다

- 단건·목록 조회는 `useQuery`, 커서/페이지 기반 목록은 `useInfiniteQuery`, 생성·수정·삭제는 `useMutation`을 사용한다.
- `useEffect` + `useState` 조합으로 API fetch, 로딩/에러 플래그, 수동 캐시를 구현하지 않는다.
- Mutation 성공 후에는 관련된 `queryKey`만 `invalidateQueries`하거나 `setQueryData`로 캐시를 갱신한다. 전체 화면을 강제 새로고침하거나 임의의 수동 `refresh()`를 연쇄 호출하지 않는다.
- Query Key는 `[appOrDomain, resource, scope, params]` 형태로 계층화하고 원시값 중심으로 안정화해, 같은 데이터를 쓰는 화면·컴포넌트끼리 요청을 자동 중복 제거(deduplication)하고 캐시를 공유하게 한다.

## 2. 불필요한 재조회와 깜빡임을 막는다

- 모든 쿼리에 데이터 특성에 맞는 `staleTime`과 `gcTime`을 명시한다. (`staleTime: 0` 기본값 방치 금지)
  - 일반 화면/목록: 30초~1분 이상
  - 자주 변하지 않는 참조 데이터(환율, 설정, 메타데이터 등): 5분~10분 이상
- 백그라운드 재조회 중 기존 캐시 데이터를 `null`로 비우지 않는다. 초기 로딩(`isPending` + 데이터 없음)과 백그라운드 갱신(`isFetching` + 기존 데이터 존재)을 구분해 스피너 깜빡임을 없앤다.
- `enabled` 조건으로 실제로 필요한 시점에만 요청을 보낸다.
  - 현재 활성 탭/섹션의 쿼리만 `enabled: true`로 둔다.
  - 로그인·권한·필수 ID가 충족되지 않으면 `enabled: false`로 막는다.
  - 부모로부터 이미 데이터나 모델을 props로 전달받는 하위 컴포넌트는 내부 쿼리를 `enabled: false`로 비활성화한다.
- 화면 포커스 복귀(`useFocusEffect` 등) 시 무조건 재조회하지 않는다.
  - 최초 마운트 포커스 이벤트는 건너뛰어 마운트 직후 2중 호출을 방지한다.
  - 재방문 포커스에서는 `isStale === true`일 때만 조용히 재조회한다.

## 3. `useEffect`는 최후의 수단으로만 사용한다

다음 용도에는 `useEffect`를 사용하지 않는다.
- **API fetch / 비동기 데이터 로딩** → `useQuery` / `useInfiniteQuery`
- **파생 값 계산·필터링·정렬** → 렌더 중 직접 계산하거나 비용이 클 때만 `useMemo`
- **버튼 클릭·폼 제출·사용자 액션 후속 처리** → 이벤트 핸들러 또는 `useMutation` 콜백(`onSuccess`/`onError`)에서 직접 실행
- **props나 다른 state 변화에 맞춰 로컬 state 동기화** → 단일 원천(derived state)으로 계산하거나, 폼 초기화가 필요하면 명시적 이벤트·컴포넌트 경계로 처리

`useEffect`는 아래처럼 React 외부 시스템과 동기화할 때만 사용한다.
- 이벤트 리스너, Realtime/WebSocket, 네이티브 모듈 구독 등록 및 cleanup 해제
- 타이머, 애니메이션, 포커스/스크롤 같은 명령형 플랫폼 API 제어
- 1회성 외부 부수효과(초기화/스토리지 확인 등) — 단, 중복 실행 가드를 함께 둔다

## 4. 리렌더링과 루트 리마운트 연쇄를 차단한다

- 계정·세션·에폭 전환 시 `<Provider key={identity}>`처럼 네비게이션이나 모달 트리를 감싸는 상위 컴포넌트를 `key`로 언마운트/리마운트하지 않는다.
  - 캐시 격리는 `queryClient.clear()`(또는 계정 스코프 키 제거)로 처리한다.
  - 내부 컨트롤러·리소스 재설정은 `useMemo` 의존성과 cleanup으로 처리해 하위 UI 트리를 유지한다.
- 자주 바뀌는 상태(인증 진행 상태, 네트워크 상태 등)를 루트 레이아웃이나 네비게이션 컨테이너가 직접 구독하지 않는다. 필요한 동기화는 작은 리프 컴포넌트로 분리한다.
- 공유 Context의 `value` 객체와 액션 함수는 `useMemo` / `useCallback`으로 참조를 고정한다.
- Realtime 구독·CAS 충돌 해결·오프라인 큐처럼 트랜잭션 상태 머신이 필요한 핵심 저장소는 전용 스토어(`useSyncExternalStore` 등)를 유지할 수 있으나, 일반 읽기/RPC/외부 HTTP 조회는 모두 TanStack Query로 분리한다.

## 5. 작업 완료 전 체크리스트

1. 새로 추가하거나 수정한 API 조회가 `useEffect`가 아닌 `useQuery` / `useInfiniteQuery` / `useMutation`을 사용하는가?
2. 비활성 탭·미로그인·무권한·외부 주입 상태에서 불필요한 요청이 나가지 않도록 `enabled`가 걸려 있는가?
3. 마운트 직후와 화면 포커스 복귀 시 같은 요청이 중복 발생하지 않으며, `staleTime` 내 재방문 시 캐시를 재사용하는가?
4. 계정 전환·상태 변경 시 루트 Provider나 모달/네비게이션 트리가 불필요하게 언마운트되거나 전체 리렌더링되지 않는가?
