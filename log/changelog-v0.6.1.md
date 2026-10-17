# ScopeWeaver v0.6.1 — Keep text-only queued edits readable

## Fixed

- Fix a chat-page crash after a queued message is edited through the API without an attachment list. Empty lists now follow the same representation as new admissions; existing queued records with null or omitted lists remain readable without a data migration. Nonempty attachments, stale-write rejection and invalid-input errors retain their behavior.
- Correct Windows browser-test expectations for native clipboard line endings and verify the actual uploaded bytes. The application already preserved the clipboard text it received. The dashboard test now launches its copied executable with the Windows `.exe` suffix.

## Upgrade and verification

This patch adds no schema changes beyond v0.6.0. Upgrades from earlier versions still apply the [v0.6.0 migration and backup requirements](https://github.com/cskwork/scopeweaver/releases/tag/v0.6.0). Stop the previous server before replacement and keep one active server per database with exclusive ownership of its data directory.

Regression coverage includes valid missing/null/empty/nonempty attachment lists, invalid non-array values, unchanged legacy stored data and revisions, and the real browser queue/edit flow. Native Windows candidate verification passed the real browser smoke, all 12 clipboard cases, queued-input/legacy-record checks, restart and 14 dashboard scenarios. The new database/API regression suite passes all 17 cases; the original released-binary failure and test-first RED results are retained. Paid-provider and other-platform runtime validation are separate from the Windows/fixture-based checks.

## 한국어

### 수정

- API로 대기 메시지를 수정할 때 첨부 목록을 생략하면 채팅 화면이 열리지 않던 문제를 수정했습니다. 빈 목록은 새 메시지 접수와 같은 형식으로 처리하며, 기존 목록이 null이거나 누락된 대기 메시지도 데이터 마이그레이션 없이 읽을 수 있습니다. 첨부가 있는 메시지, 오래된 리비전의 수정 거부, 잘못된 입력의 오류 처리는 유지합니다.
- Windows 클립보드의 줄바꿈 변환을 브라우저 테스트에 반영하고 실제 업로드된 파일의 바이트도 확인합니다. 앱은 전달받은 클립보드 내용을 이미 그대로 보존하고 있었습니다. 대시보드 테스트는 Windows에서 복사한 실행 파일에 `.exe` 확장자를 사용합니다.

### 업데이트와 검증

v0.6.0에 추가되는 스키마 변경은 없습니다. 더 이전 버전에서 업데이트하면 [v0.6.0의 마이그레이션과 백업 안내](https://github.com/cskwork/scopeweaver/releases/tag/v0.6.0)가 적용됩니다. 교체 전에 이전 서버를 중지하고, 데이터베이스마다 서버 하나만 실행하며 데이터 디렉터리를 단독으로 사용하세요.

회귀 검증에는 첨부 목록의 누락·null·빈 배열·첨부가 있는 배열, 배열이 아닌 잘못된 값, 기존 저장 데이터와 리비전 보존, 실제 브라우저의 대기 메시지 수정 흐름이 포함됩니다. Windows 후보 실행 파일에서 실제 브라우저 스모크, 클립보드 12개 경우, 대기 메시지와 기존 저장 데이터 검사, 재시작, 대시보드 시나리오 14개를 통과했습니다. 새 데이터베이스/API 회귀 검증 17개도 통과했으며, 기존 배포 파일의 실패와 수정 전 실패 기록을 보존했습니다. 유료 제공자와 다른 운영체제의 실행 검증은 Windows 및 로컬 고정 응답 기반 검사와 구분합니다.

[Compare v0.6.0...v0.6.1](https://github.com/cskwork/scopeweaver/compare/v0.6.0...v0.6.1)
