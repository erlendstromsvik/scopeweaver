package locale

// Add only missing shared messages; domain-specific catalogs remain authoritative.
func init() {
	if !Has("action must be pause|resume") {
		Register("action must be pause|resume", "action must be pause|resume", "동작은 pause 또는 resume여야 합니다")
	}
	if !Has("action must be pause|resume|cancel") {
		Register("action must be pause|resume|cancel", "action must be pause|resume|cancel", "동작은 pause, resume 또는 cancel이어야 합니다")
	}
	if !Has("agent not found") {
		Register("agent not found", "agent not found", "에이전트를 찾을 수 없습니다")
	}
	if !Has("api_key required") {
		Register("api_key required", "api_key required", "api_key가 필요합니다")
	}
	if !Has("asset is not associated with this task") {
		Register("asset is not associated with this task", "asset is not associated with this task", "자산이 이 작업과 연결되지 않았습니다")
	}
	if !Has("bad asset id") {
		Register("bad asset id", "bad asset id", "자산 ID가 잘못되었습니다")
	}
	if !Has("bad constraint id") {
		Register("bad constraint id", "bad constraint id", "제약 ID가 잘못되었습니다")
	}
	if !Has("bad conversation id") {
		Register("bad conversation id", "bad conversation id", "대화 ID가 잘못되었습니다")
	}
	if !Has("bad finding id") {
		Register("bad finding id", "bad finding id", "취약점 ID가 잘못되었습니다")
	}
	if !Has("bad goal id") {
		Register("bad goal id", "bad goal id", "목표 ID가 잘못되었습니다")
	}
	if !Has("bad id") {
		Register("bad id", "bad id", "ID가 잘못되었습니다")
	}
	if !Has("bad intent id") {
		Register("bad intent id", "bad intent id", "의도 ID가 잘못되었습니다")
	}
	if !Has("bad json") {
		Register("bad json", "bad json", "JSON이 잘못되었습니다")
	}
	if !Has("bad seq") {
		Register("bad seq", "bad seq", "순번이 잘못되었습니다")
	}
	if !Has("bad session") {
		Register("bad session", "bad session", "세션이 잘못되었습니다")
	}
	if !Has("bad task category id") {
		Register("bad task category id", "bad task category id", "작업 분류 ID가 잘못되었습니다")
	}
	if !Has("bad task template id") {
		Register("bad task template id", "bad task template id", "작업 템플릿 ID가 잘못되었습니다")
	}
	if !Has("bad trigger id") {
		Register("bad trigger id", "bad trigger id", "트리거 ID가 잘못되었습니다")
	}
	if !Has("category_id is required") {
		Register("category_id is required", "category_id is required", "category_id가 필요합니다")
	}
	if !Has("context task not found") {
		Register("context task not found", "context task not found", "문맥 작업을 찾을 수 없습니다")
	}
	if !Has("conversation not found") {
		Register("conversation not found", "conversation not found", "대화를 찾을 수 없습니다")
	}
	if !Has("description is required") {
		Register("description is required", "description is required", "description이 필요합니다")
	}
	if !Has("description must be at most %d characters") {
		Register("description must be at most %d characters", "description must be at most %d characters", "설명은 최대 %d자여야 합니다")
	}
	if !Has("file not found") {
		Register("file not found", "file not found", "파일을 찾을 수 없습니다")
	}
	if !Has("finding not available in task context") {
		Register("finding not available in task context", "finding not available in task context", "작업 문맥에서 취약점을 사용할 수 없습니다")
	}
	if !Has("finding not found") {
		Register("finding not found", "finding not found", "취약점을 찾을 수 없습니다")
	}
	if !Has("finding origin task is no longer available") {
		Register("finding origin task is no longer available", "finding origin task is no longer available", "취약점의 원본 작업을 더 이상 사용할 수 없습니다")
	}
	if !Has("finding origin task or node is no longer available") {
		Register("finding origin task or node is no longer available", "finding origin task or node is no longer available", "취약점의 원본 작업 또는 노드를 더 이상 사용할 수 없습니다")
	}
	if !Has("invalid JSON") {
		Register("invalid JSON", "invalid JSON", "JSON이 잘못되었습니다")
	}
	if !Has("invalid binding id") {
		Register("invalid binding id", "invalid binding id", "연결 ID가 잘못되었습니다")
	}
	if !Has("invalid body") {
		Register("invalid body", "invalid body", "본문이 잘못되었습니다")
	}
	if !Has("invalid finding id") {
		Register("invalid finding id", "invalid finding id", "취약점 ID가 잘못되었습니다")
	}
	if !Has("invalid scope id") {
		Register("invalid scope id", "invalid scope id", "범위 ID가 잘못되었습니다")
	}
	if !Has("invalid skill name") {
		Register("invalid skill name", "invalid skill name", "스킬 이름이 잘못되었습니다")
	}
	if !Has("missing hash") {
		Register("missing hash", "missing hash", "hash가 필요합니다")
	}
	if !Has("missing host") {
		Register("missing host", "missing host", "host가 필요합니다")
	}
	if !Has("missing hosts") {
		Register("missing hosts", "missing hosts", "hosts가 필요합니다")
	}
	if !Has("missing id") {
		Register("missing id", "missing id", "id가 필요합니다")
	}
	if !Has("missing task") {
		Register("missing task", "missing task", "작업이 필요합니다")
	}
	if !Has("no findings selected") {
		Register("no findings selected", "no findings selected", "선택된 취약점이 없습니다")
	}
	if !Has("no task") {
		Register("no task", "no task", "작업이 없습니다")
	}
	if !Has("node is not an intent") {
		Register("node is not an intent", "node is not an intent", "노드가 의도가 아닙니다")
	}
	if !Has("nothing to update: provide status/severity/name/vulnclass") {
		Register("nothing to update: provide status/severity/name/vulnclass", "nothing to update: provide status/severity/name/vulnclass", "갱신할 내용이 없습니다. status/severity/name/vulnclass를 제공하세요")
	}
	if !Has("saved provider is unavailable") {
		Register("saved provider is unavailable", "saved provider is unavailable", "저장된 공급자를 사용할 수 없습니다")
	}
	if !Has("scope row not found") {
		Register("scope row not found", "scope row not found", "범위 항목을 찾을 수 없습니다")
	}
	if !Has("session not found") {
		Register("session not found", "session not found", "세션을 찾을 수 없습니다")
	}
	if !Has("side question not found") {
		Register("side question not found", "side question not found", "보조 질문을 찾을 수 없습니다")
	}
	if !Has("skill already exists") {
		Register("skill already exists", "skill already exists", "스킬이 이미 있습니다")
	}
	if !Has("skill name must be 1-64 lowercase alphanumeric/hyphen characters, not starting/ending/doubling hyphens") {
		Register("skill name must be 1-64 lowercase alphanumeric/hyphen characters, not starting/ending/doubling hyphens", "skill name must be 1-64 lowercase alphanumeric/hyphen characters, not starting/ending/doubling hyphens", "스킬 이름은 소문자, 숫자, 하이픈 1~64자이며 하이픈으로 시작/끝나거나 연속 하이픈을 포함할 수 없습니다")
	}
	if !Has("skill not found") {
		Register("skill not found", "skill not found", "스킬을 찾을 수 없습니다")
	}
	if !Has("streaming unavailable") {
		Register("streaming unavailable", "streaming unavailable", "스트리밍을 사용할 수 없습니다")
	}
	if !Has("streaming unsupported") {
		Register("streaming unsupported", "streaming unsupported", "스트리밍을 지원하지 않습니다")
	}
	if !Has("task is being deleted") {
		Register("task is being deleted", "task is being deleted", "작업을 삭제 중입니다")
	}
	if !Has("traffic disabled") {
		Register("traffic disabled", "traffic disabled", "트래픽 수집이 꺼져 있습니다")
	}
	if !Has("bad json: ") {
		Register("bad json: ", "bad json: ", "잘못된 JSON: ")
	}
	if !Has("provider init failed: ") {
		Register("provider init failed: ", "provider init failed: ", "공급자 초기화 실패: ")
	}
	if !Has("persist provider failed: ") {
		Register("persist provider failed: ", "persist provider failed: ", "공급자 저장 실패: ")
	}
	if !Has("bad finding id: ") {
		Register("bad finding id: ", "bad finding id: ", "잘못된 취약점 ID: ")
	}
	if !Has("bad scope: ") {
		Register("bad scope: ", "bad scope: ", "잘못된 범위: ")
	}
	if !Has("bad format: ") {
		Register("bad format: ", "bad format: ", "잘못된 형식: ")
	}
	if !Has("bad status: ") {
		Register("bad status: ", "bad status: ", "잘못된 상태: ")
	}
	if !Has("bad severity: ") {
		Register("bad severity: ", "bad severity: ", "잘못된 심각도: ")
	}
	if !Has("unknown %s ref: %s") {
		Register("unknown %s ref: %s", "unknown %s ref: %s", "알 수 없는 %s 참조: %s")
	}
	if !Has("invalid JSON: ") {
		Register("invalid JSON: ", "invalid JSON: ", "잘못된 JSON: ")
	}
	if !Has("db: ") {
		Register("db: ", "db: ", "데이터베이스: ")
	}
	if !Has("empty body from model") {
		Register("empty body from model", "empty body from model", "모델이 빈 본문을 반환했습니다")
	}
	if !Has("invalid evidence hash") {
		Register("invalid evidence hash", "invalid evidence hash", "증거 해시가 잘못되었습니다")
	}
	if !Has("restore destination already exists: %s") {
		Register("restore destination already exists: %s", "restore destination already exists: %s", "복원 대상이 이미 있습니다: %s")
	}
	if !Has("inspect restore destination %s: %w") {
		Register("inspect restore destination %s: %w", "inspect restore destination %s: %w", "복원 대상 %s 확인: %w")
	}
	if !Has("restore %s: %w") {
		Register("restore %s: %w", "restore %s: %w", "%s 복원: %w")
	}
	if !Has("remove traffic stage: %w") {
		Register("remove traffic stage: %w", "remove traffic stage: %w", "트래픽 준비 영역 제거: %w")
	}
	if !Has("archive and task ids must be positive") {
		Register("archive and task ids must be positive", "archive and task ids must be positive", "보관 ID와 작업 ID는 양수여야 합니다")
	}
	if !Has("unsupported traffic archive version %d") {
		Register("unsupported traffic archive version %d", "unsupported traffic archive version %d", "지원하지 않는 트래픽 보관 버전 %d")
	}
	if !Has("invalid archived traffic blob %q") {
		Register("invalid archived traffic blob %q", "invalid archived traffic blob %q", "보관된 트래픽 blob %q이(가) 잘못되었습니다")
	}
	if !Has("traffic blob checksum mismatch: %s") {
		Register("traffic blob checksum mismatch: %s", "traffic blob checksum mismatch: %s", "트래픽 blob 체크섬 불일치: %s")
	}
	if !Has("llm: invalid proxy %q: %w") {
		Register("llm: invalid proxy %q: %w", "llm: invalid proxy %q: %w", "llm: 잘못된 프록시 %q: %w")
	}
	if !Has("llm: proxy %q missing scheme (use http://, https:// or socks5://)") {
		Register("llm: proxy %q missing scheme (use http://, https:// or socks5://)", "llm: proxy %q missing scheme (use http://, https:// or socks5://)", "llm: 프록시 %q에 프로토콜이 없습니다(http://, https:// 또는 socks5:// 사용)")
	}
	if !Has("llm: unsupported proxy scheme %q (use http, https or socks5)") {
		Register("llm: unsupported proxy scheme %q (use http, https or socks5)", "llm: unsupported proxy scheme %q (use http, https or socks5)", "llm: 지원하지 않는 프록시 프로토콜 %q(http, https 또는 socks5 사용)")
	}
	if !Has("invalid HTTP header name %q") {
		Register("invalid HTTP header name %q", "invalid HTTP header name %q", "잘못된 HTTP 헤더 이름 %q")
	}
	if !Has("mcp sse http %d: %s") {
		Register("mcp sse http %d: %s", "mcp sse http %d: %s", "mcp sse http %d: %s")
	}
	if !Has("mcp sse endpoint: %w") {
		Register("mcp sse endpoint: %w", "mcp sse endpoint: %w", "mcp sse 엔드포인트: %w")
	}
	if !Has("mcp sse endpoint URL: %w") {
		Register("mcp sse endpoint URL: %w", "mcp sse endpoint URL: %w", "mcp sse 엔드포인트 URL: %w")
	}
	if !Has("bad MCP response: %w") {
		Register("bad MCP response: %w", "bad MCP response: %w", "잘못된 MCP 응답: %w")
	}
	if !Has("mcp tool %q error: %s") {
		Register("mcp tool %q error: %s", "mcp tool %q error: %s", "mcp 도구 %q 오류: %s")
	}
	if !Has("mcp: empty response for %s") {
		Register("mcp: empty response for %s", "mcp: empty response for %s", "mcp: %s의 빈 응답")
	}
	if !Has("mcp http %d: %s") {
		Register("mcp http %d: %s", "mcp http %d: %s", "mcp http %d: %s")
	}
	if !Has("mcp: decode json response: %w") {
		Register("mcp: decode json response: %w", "mcp: decode json response: %w", "mcp: JSON 응답 해석: %w")
	}
	if !Has("mcp sse stream: %w") {
		Register("mcp sse stream: %w", "mcp sse stream: %w", "mcp sse 스트림: %w")
	}
	if !Has("mcp: no matching response in event stream") {
		Register("mcp: no matching response in event stream", "mcp: no matching response in event stream", "mcp: 이벤트 스트림에 일치하는 응답이 없습니다")
	}
}
