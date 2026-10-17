# ScopeWeaver v0.6.0 — Chat controls, durable findings and cloud token accounting

## Added

- **Chat controls for standalone and task conversations.** Queue, edit, reorder or cancel pending messages; pause and resume the queue; steer an active tool run; retry an answer, edit an earlier prompt or branch a conversation. Interrupted runs remain visible for acknowledgement before queued work resumes. Shortcuts include `/queue`, `/steer`, `/retry`, `/branch`, `/context`, `/usage` and `/compress`. See [#29](https://github.com/cskwork/scopeweaver/pull/29).
- **Context inspection and compression.** Inspect the model's current context separately from billed usage, then compress it while retaining your selected recent completed exchanges. Display history and usage from earlier attempts remain available.
- **Document paste.** Paste clipboard files as attachments. Text longer than 3,000 characters becomes a text attachment; shorter text stays in the composer. Attachment chips show pending files, and failed uploads preserve the pasted text.
- **Durable standalone findings.** Penetration-testing conversations can save confirmed findings to the Findings page under the unassigned-task group and retrieve their own finding history across turns, without creating a task. See [#30](https://github.com/cskwork/scopeweaver/pull/30).
- **Cloud economy templates.** Add DeepSeek, Claude Haiku, OpenAI Responses, Qwen, Kimi and OpenRouter templates with a 16,384-token output limit and the default compaction window. Z.ai GLM templates now use low effort with the same output limit. Selecting a template retains an entered API key and does not save, activate or contact a provider. See [#27](https://github.com/cskwork/scopeweaver/pull/27) and [token controls](../docs/llm-providers.md#controlling-token-use).

## Fixed

- Missing or invalid Auto, Penetration testing, Report writer and Finding retest prompts fall back to their own role defaults. Translated render-failure diagnostics avoid logging the prompt body.
- Preserve legacy task-chat response shapes, blank-prompt behavior and original history when retrying or branching. Legacy synchronous replies have bounded waits; a failed claim cancels an unchanged queued input, while claimed or revised work remains preserved. Protect each session's draft and attachments while Queue or Steer is awaiting acknowledgement; keep drafts separate when creating another session.
- Copy branch attachments correctly on Windows, preserve child attachments after parent deletion, and reject unsafe paths and symlink destinations. Startup reconciles unpublished transcript files before resuming queues, conservatively preserving files whose ownership or publication state is uncertain. These review repairs were integrated through [#32](https://github.com/cskwork/scopeweaver/pull/32) and promoted through [#33](https://github.com/cskwork/scopeweaver/pull/33).
- **Cloud request and usage handling.** Mark Anthropic system prompts as cacheable, handle official Claude 5.5 adaptive thinking and tool-loop replay, and correct cumulative streamed-token accounting. Record provider cache-read, cache-write and reasoning fields; dashboard, task overview and approval usage show reasoning as part of output. Completed OpenAI Responses side questions remain completed. DeepSeek web searches and settings probes are metered, and invalid proxy or CA configuration fails explicitly. These fixes change ScopeWeaver's accounting; they do not change a provider's bill. See [#27](https://github.com/cskwork/scopeweaver/pull/27).
- Database-writing test suites require an explicit disposable `ARTEX_TEST_DSN` and refuse the live database. See [#26](https://github.com/cskwork/scopeweaver/pull/26).

## Upgrade and compatibility

**Back up the PostgreSQL database and data directory before upgrading, then stop the previous server before starting v0.6.0.** Run only one active server per PostgreSQL database, with exclusive ownership of its data directory. Concurrent replicas sharing either resource are unsupported. See [updating](../README.md#updating) and [installation](../README.md#installation).

Startup applies schema migrations automatically. This release adds seven durable chat tables, conversation-owned finding linkage and reasoning-token usage storage. Current context snapshots live on chat sessions; an intermediate `chat_context_snapshots` table is migrated and retired only after matching session ownership and payloads are checked. Conflicting migration data stops the upgrade rather than choosing one copy. Revision checks reject stale history or context changes. Compatibility tests cover old conversation/task history, repeated startup migration, v3 archives and v4 archives with paused pending work, including older v4 snapshot-table data. Database schemas do not roll back with a binary rollback; retain the backup for recovery. No live user database was migrated as part of release preparation.

New controls and diagnostics are available in English, Korean and Simplified Chinese. Existing user content, saved evidence and historical records are not translated.

## Verification and limits

The reviewed candidate `00bfd1a`, integrated `dev` commit `bacfd7c` and promoted `main` commit `933c7d8` passed all seven required jobs: branch policy, frontend, agent adapter, backend, Windows, chat browser and smoke. Recorded results include 65 frontend tests, 7 adapter unit tests plus 3 real MCP/Pi scenarios, 60 standard chat browser steps plus 5 draft-retention steps, embedded-app smoke and 14 dashboard scenarios. All 12 review scenarios passed, including old-data upgrades, archive roundtrips, context revision conflicts and startup recovery. See the exact-main [checks](https://github.com/cskwork/scopeweaver/actions/runs/38979809062) and [E2E](https://github.com/cskwork/scopeweaver/actions/runs/38979809058). Release packaging and publication are separate workflow gates.

Browser checks used Chromium at desktop/mobile viewport widths with loopback model/tool fixtures and disposable databases. File-paste coverage uses synthetic clipboard events; native operating-system file-clipboard behavior was not verified. `TestLiveContextReview` remained skipped because it requires an external private model configuration. No paid-provider E2E or native-speaker translation approval is claimed. Cloud wire/usage mappings are fixture-verified; provider costs and quality need separate validation. The documented GLM economy measurement is a small task-specific result and does not establish savings for other providers. See [verification history and limitations](../docs/VERIFICATION.md) and [provider evidence](../docs/llm-providers.md#controlling-token-use).

Thanks to [@xKeNcHii](https://github.com/xKeNcHii) for chat controls, [@ajul8866](https://github.com/ajul8866) for standalone findings and role fallback, [@erlendstromsvik](https://github.com/erlendstromsvik) for database-test isolation, and [@cskwork](https://github.com/cskwork) for cloud fixes, integration and review repairs.

## 한국어

### 추가

- **독립 대화와 작업 대화의 채팅 제어:** 대기 메시지를 추가·수정·재정렬·취소하고 큐를 일시 정지하거나 재개할 수 있습니다. 실행 중인 도구에 방향을 전달하고, 답변을 재시도하거나 이전 질문을 수정하거나 대화를 분기할 수 있습니다. 중단된 실행은 확인할 때까지 표시하며, 확인 후 대기 작업을 재개합니다. `/queue`, `/steer`, `/retry`, `/branch`, `/context`, `/usage`, `/compress` 명령을 지원합니다. [#29](https://github.com/cskwork/scopeweaver/pull/29)를 참고하세요.
- **문맥 조회와 압축:** 청구 사용량과 별도로 모델의 현재 문맥을 확인하고, 선택한 최근 완료 대화를 남기며 압축할 수 있습니다. 화면의 전체 기록과 이전 시도의 사용량은 유지합니다.
- **문서 붙여넣기:** 클립보드 파일을 첨부할 수 있습니다. 3,000자를 초과하는 텍스트는 텍스트 첨부 파일로, 짧은 텍스트는 입력창 내용으로 처리합니다. 대기 첨부 파일을 표시하며 업로드에 실패해도 붙여넣은 텍스트를 보존합니다.
- **독립 대화의 취약점 영구 저장:** 모의 해킹 대화에서 확인한 취약점을 작업 생성 없이 취약점 페이지의 미할당 작업 그룹에 저장하고, 같은 대화의 이전 취약점 기록을 다시 조회할 수 있습니다. [#30](https://github.com/cskwork/scopeweaver/pull/30)을 참고하세요.
- **클라우드 절약형 템플릿:** DeepSeek·Claude Haiku·OpenAI Responses·Qwen·Kimi·OpenRouter 템플릿에 출력 한도 16,384토큰과 기본 문맥 압축 창을 적용했습니다. Z.ai GLM 템플릿도 낮은 추론 강도와 같은 출력 한도를 사용합니다. 템플릿을 선택해도 입력한 API 키는 유지하며, 제공자 연결·저장·활성화를 수행하지 않습니다. [#27](https://github.com/cskwork/scopeweaver/pull/27)과 [토큰 사용량 제어](../docs/ko/llm-providers.md)를 참고하세요.

### 수정

- Auto·모의 해킹·보고서 작성·취약점 재검증 프롬프트가 없거나 유효하지 않으면 각 역할의 기본 프롬프트를 사용합니다. 번역된 렌더링 실패 진단에서 프롬프트 본문을 로그에 남기지 않습니다.
- 기존 작업 채팅의 응답 형식과 빈 질문 동작을 유지하고, 재시도·분기 시 원본 기록을 보존합니다. 기존 동기 응답의 대기 시간을 제한합니다. 실행 선점에 실패하면 변경되지 않은 대기 입력을 취소하고, 이미 선점했거나 수정한 작업은 보존합니다. Queue·Steer 요청의 응답을 기다리는 동안 세션의 초안과 첨부 파일을 보호하며, 다른 세션을 만들어도 초안을 서로 분리해 유지합니다.
- Windows에서 분기 첨부 파일을 올바르게 복사하고, 부모를 삭제해도 자식 첨부 파일을 보존하며, 안전하지 않은 경로와 심볼릭 링크 목적지를 거부합니다. 시작 시 큐 재개 전에 미게시 대화 기록 파일을 정리하되 소유권·게시 상태가 불확실한 파일은 보존합니다. 검토 수정은 [#32](https://github.com/cskwork/scopeweaver/pull/32)를 통해 통합하고 [#33](https://github.com/cskwork/scopeweaver/pull/33)을 통해 승격했습니다.
- **클라우드 요청과 사용량 처리:** Anthropic 시스템 프롬프트에 캐시 표시를 적용하고, 공식 Claude 5.5의 적응형 추론과 도구 반복 실행의 추론 블록 재전달을 처리하며, 누적 스트림 토큰의 중복 합산을 수정했습니다. 제공자의 캐시 읽기·쓰기·추론 토큰을 기록하고 대시보드·작업 개요·승인 사용량에 추론을 출력의 일부로 표시합니다. 완료된 OpenAI Responses 곁가지 질문을 중단으로 표시하지 않습니다. DeepSeek 웹 검색과 설정 화면 검사도 사용량에 기록하며, 잘못된 프록시·CA 설정은 명시적으로 실패합니다. 이는 ScopeWeaver의 집계를 고치는 변경이며 제공자의 청구액을 바꾸지 않습니다. [#27](https://github.com/cskwork/scopeweaver/pull/27)을 참고하세요.
- 데이터베이스에 쓰는 테스트는 일회용 `ARTEX_TEST_DSN`을 명시해야 하며 실제 운영 데이터베이스를 거부합니다. [#26](https://github.com/cskwork/scopeweaver/pull/26)을 참고하세요.

### 업그레이드와 호환성

**업그레이드 전에 PostgreSQL 데이터베이스와 데이터 디렉터리를 백업하고, 기존 서버를 중지한 뒤 v0.6.0을 시작하세요.** PostgreSQL 데이터베이스마다 활성 서버는 하나만 실행하고 데이터 디렉터리도 단독으로 사용해야 합니다. 둘 중 하나라도 공유하는 동시 복제본은 지원하지 않습니다. [업데이트](../README.ko.md#업데이트)와 [설치](../README.ko.md#설치)를 참고하세요.

시작 시 스키마를 자동으로 전환합니다. 이번 릴리스는 영구 채팅 테이블 7개, 대화 소유 취약점 연결, 추론 토큰 저장 공간을 추가합니다. 현재 문맥 스냅샷은 채팅 세션에 저장합니다. 중간 버전의 `chat_context_snapshots` 테이블이 있으면 세션 소유권과 내용을 확인한 뒤 옮기고 기존 테이블을 제거합니다. 전환 데이터가 충돌하면 한쪽을 선택하지 않고 업그레이드를 중단합니다. 리비전 검사로 오래된 기록·문맥 변경을 거부합니다. 기존 대화·작업 기록, 반복 시작 시 전환, v3 아카이브, 일시 정지된 대기 작업을 포함한 v4 아카이브와 이전 v4 스냅샷 테이블 형식을 호환성 테스트로 확인했습니다. 바이너리를 되돌려도 데이터베이스 스키마는 되돌아가지 않으므로 복구용 백업을 보관하세요. 릴리스 준비 과정에서 실제 사용자 데이터베이스를 전환하지 않았습니다.

새 제어 기능과 진단은 영어·한국어·중국어 간체로 제공합니다. 사용자 내용·저장된 증거·과거 기록은 번역하지 않습니다.

### 검증과 한계

검토 후보 `00bfd1a`, 통합 `dev` 커밋 `bacfd7c`, 승격한 `main` 커밋 `933c7d8`에서 필수 검사 7개인 브랜치 정책·프런트엔드·에이전트 어댑터·백엔드·Windows·채팅 브라우저·스모크가 모두 통과했습니다. 프런트엔드 테스트 65개, 어댑터 단위 테스트 7개와 실제 MCP/Pi 시나리오 3개, 표준 채팅 브라우저 단계 60개와 초안 보존 단계 5개, 내장 앱 스모크와 대시보드 시나리오 14개를 확인했습니다. 기존 데이터 전환·아카이브 왕복·문맥 리비전 충돌·시작 복구를 포함한 검토 시나리오 12개가 모두 통과했습니다. 해당 main의 [검사](https://github.com/cskwork/scopeweaver/actions/runs/38979809062)와 [E2E](https://github.com/cskwork/scopeweaver/actions/runs/38979809058)를 참고하세요. 릴리스 패키징과 게시는 별도 워크플로 검사입니다.

브라우저 검사는 데스크톱·모바일 화면 너비의 Chromium, 루프백 모델·도구, 일회용 데이터베이스를 사용했습니다. 파일 붙여넣기는 합성 클립보드 이벤트로 검사했으며 운영체제의 실제 파일 클립보드는 확인하지 않았습니다. 외부 비공개 모델 설정이 필요한 `TestLiveContextReview`는 건너뛰었습니다. 유료 제공자의 E2E나 원어민 번역 승인을 주장하지 않습니다. 클라우드 요청·사용량 매핑은 고정 응답으로 검증했으며 제공자별 비용·품질은 별도 검증이 필요합니다. 문서의 GLM 절약 측정은 소규모 특정 작업의 결과이며 다른 제공자의 절약을 입증하지 않습니다. [검증 기록과 한계](../docs/ko/VERIFICATION.md), [제공자 증거](../docs/ko/llm-providers.md)를 참고하세요.

채팅 제어를 기여한 [@xKeNcHii](https://github.com/xKeNcHii), 독립 대화 취약점과 역할 기본값 복원을 기여한 [@ajul8866](https://github.com/ajul8866), 데이터베이스 테스트 격리를 기여한 [@erlendstromsvik](https://github.com/erlendstromsvik), 클라우드 수정·통합·검토 수정을 수행한 [@cskwork](https://github.com/cskwork)에게 감사드립니다.

---

[Compare v0.5.0...v0.6.0](https://github.com/cskwork/scopeweaver/compare/v0.5.0...v0.6.0)
