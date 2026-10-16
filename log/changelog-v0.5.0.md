# ScopeWeaver v0.5.0 — Safer local Ollama inference

## Changed

- **Native request priority.** Queued approval-judge, main-agent and side-question requests receive the next free Ollama slot before background work. Each group remains FIFO; active inference is never interrupted. Diagnostics and pre-warm remain background work.
- **Approval judge selection and failure policy.** An explicitly selected judge profile wins; otherwise judging follows the actual native profile used by the originating run, including pool and failover selection. Native defaults are now 90 seconds and `ask`, with saved overrides taking priority. Late stream errors and terminal routing errors retain the native profile and its failure policy instead of inheriting a cloud `allow` default. Human approval still expires after 300 seconds with `deny` by default. Native verdicts use a decision/comment JSON schema and remain validated.
- **Stricter optional local-only networking.** With `local_llm_only` enabled, LLM HTTP permits loopback and explicit trusted CIDRs only. Private LAN, IPv6 ULA and Docker addresses are no longer implicitly trusted. Link-local, multicast and unspecified destinations are always refused; resolved addresses and socket destinations are checked, and all redirects and LLM profile proxies are refused.
- **Live search and update controls.** Strict mode now blocks every agent web-search backend and connectivity probe, plus update checks and downloads, including existing sessions. A later strict-mode toggle stops an admitted update at the next guarded network operation/body read, cleans partial staging and completes its failure state. Local version status, update progress/SSE and rollback remain available; cached release data cannot bypass the policy.
- **Small-context safeguards.** Native context windows through 64K compact history at 72%, preserving complete tool call/result pairs. Per-tool text sent to the model is capped at 16 KiB through 32K, and 32 KiB above 32K through 64K, measured in bytes. Full output remains in a referenced spill file. These native safeguards take priority over experimental noa compaction.
- **More useful diagnostics.** Connection testing shows the selected model's reported GPU-memory share and warns about 80%-or-lower GPU share, other loaded models, and a judge using a different model/context on the same server. Missing measurements remain unknown. The diagnostic tool probe disables thinking without changing the profile's inference settings.

## Fixed

- Native tool arguments accept objects and JSON-encoded objects, reject malformed/non-object values explicitly, and preserve large arguments, tool-result replay, cached-token usage and raw timing evidence. Late stream errors remain errors after partial output; ordinary text never becomes a tool call.
- Approval settings cannot save defaults while the stored configuration is loading or has failed to load; retry remains available. The profile editor now fits a 390px viewport. Diagnostics, warnings and strict-update messages are available in English, Korean and Simplified Chinese.

See [#25](https://github.com/cskwork/scopeweaver/pull/25) and the [provider guide](../docs/llm-providers.md).

## Compatibility, migration and limits

**Existing strict LAN/Docker profiles need an explicit CIDR before reconnecting.** Add the inference server's actual address to **Additional trusted CIDRs**, `SCOPEWEAVER_LOCAL_LLM_ALLOWED_CIDRS`, or retained `ARTEX_LOCAL_LLM_ALLOWED_CIDRS`; prefer IPv4 `/32` or IPv6 `/128`. For Docker, resolve the address from the ScopeWeaver container. Neither `host.docker.internal` nor `ollama` grants implicit trust. Same-machine loopback needs no entry. See the [migration instructions](../docs/llm-providers.md#network-policy-local_llm_only).

The policy remains off by default. Native `/api/chat`, keyless local profiles and cloud-provider support introduced in earlier releases remain available. Anthropic, OpenAI Chat Completions, OpenAI Responses and their existing configuration paths remain supported. This release changes no dependencies or database schema.

These guards are application controls, not whole-host or cryptographic zero-egress protection. Target tools and the global proxy retain network access, and traffic already sent cannot be recalled. Host/remote Ollama must separately disable onward cloud use with `OLLAMA_NO_CLOUD=1`; ScopeWeaver cannot control that server's forwarding. GPU-memory ratios are estimates, not exact weight placement, GPU capacity or residency guarantees. Near-context-limit warnings are heuristics, not guaranteed truncation detection. Protocol checks do not establish autonomous-task or security-assessment quality.

## Verification

Pre-merge evidence for `d4b93a3`: 1,366 Go tests/subtests passed with two documented opt-in skips; the adapter was separately checked with 7 unit tests and 3 real MCP/Pi scenarios. Frontend checks passed 45 tests, and the rebuilt embedded app passed 60 browser checks, including desktop/mobile EN/KO/zh-Hans coverage. Focused race checks, vet, static builds and all five PR checks passed. Post-merge and release checks are separate gates. See [verification evidence and limitations](../docs/VERIFICATION.md).

Existing host Ollama 0.32.15 with `qwen3:4b` passed native diagnostics, decision/comment schema, structured tool call/result replay, usage and a 32,768-token allocated context with a 4,096-token output budget. Earlier 256/512-token attempts failed and remain recorded. No model was downloaded; this was a bounded protocol exercise, not a task-quality benchmark. `TestLiveContextReview` and paid-provider calls were not run.

## 한국어

### 변경

- **네이티브 요청 우선순위:** 대기 중인 승인 판정·주 에이전트·곁가지 질문 요청에 백그라운드 작업보다 다음 빈 Ollama 슬롯을 먼저 배정합니다. 각 그룹 안에서는 선입선출을 유지하며 진행 중인 추론을 중단하지 않습니다. 진단과 예열은 백그라운드 작업으로 유지합니다.
- **승인 판정 프로필과 실패 정책:** 명시적으로 고른 승인 프로필이 우선하며, 별도 선택이 없으면 풀·장애 전환을 포함해 해당 실행에서 실제로 사용한 네이티브 프로필을 따릅니다. 네이티브 기본값은 90초·`ask`이며 저장한 설정이 우선합니다. 늦은 스트림 오류와 최종 라우팅 오류에서도 네이티브 프로필과 실패 정책을 유지해 클라우드의 `allow` 기본값을 적용하지 않습니다. 사람 승인 대기는 기본 300초 후 `deny`로 끝납니다. 네이티브 판정에는 decision/comment JSON 스키마를 적용하고 결과도 검증합니다.
- **선택형 로컬 전용 네트워크 강화:** `local_llm_only`를 켜면 LLM HTTP는 루프백과 명시적으로 신뢰한 CIDR만 허용합니다. 사설 LAN·IPv6 ULA·Docker 주소를 자동으로 신뢰하지 않습니다. 링크 로컬·멀티캐스트·지정되지 않은 주소는 항상 거부하며, 이름으로 확인한 주소와 실제 소켓 목적지를 검사하고 모든 리디렉션과 LLM 프로필 프록시를 거부합니다.
- **실시간 검색·업데이트 제어:** 엄격 모드에서 모든 Agent 웹 검색 백엔드·연결 검사와 업데이트 확인·다운로드를 차단합니다. 이미 만든 세션에도 적용됩니다. 다운로드를 시작한 뒤 엄격 모드를 켜면 다음 네트워크 동작·본문 읽기 검사에서 중단하고 임시 파일을 정리한 뒤 실패 상태로 완료합니다. 로컬 버전 확인·업데이트 진행 상황/SSE·롤백은 유지하며, 캐시된 릴리스 정보로 정책을 우회할 수 없습니다.
- **작은 문맥 보호:** 네이티브 문맥 창이 64K 이하이면 72%에서 기록을 압축하고 도구 호출·결과 쌍을 보존합니다. 모델에 전달하는 도구별 텍스트는 32K 이하에서 16KiB, 32K 초과 64K 이하에서 32KiB로 제한하며 바이트 단위로 계산합니다. 전체 출력은 결과에 경로를 표시한 별도 파일에 보존합니다. 이 네이티브 보호 장치는 실험적 noa 압축보다 우선합니다.
- **진단 개선:** 연결 테스트에서 선택한 모델의 보고된 GPU 메모리 비율을 표시하고, GPU 비율이 80% 이하이거나 다른 모델도 로드되어 있거나 같은 서버의 승인 판정 모델·문맥 설정이 다르면 경고합니다. 측정값이 없으면 알 수 없음으로 표시합니다. 진단용 도구 검사는 추론을 끄되 프로필의 실제 추론 설정은 바꾸지 않습니다.

### 수정

- 네이티브 도구 인자는 객체와 JSON 문자열로 인코딩한 객체를 받고, 잘못된 형식·객체가 아닌 값은 명시적 오류로 처리합니다. 큰 인자·도구 결과 재전달·캐시 토큰 사용량·원시 시간 정보를 보존합니다. 일부 출력 후 발생한 스트림 오류도 오류로 유지하며 일반 텍스트를 도구 호출로 바꾸지 않습니다.
- 저장된 승인 설정을 불러오는 중이거나 불러오지 못했을 때 기본값을 저장하지 못하도록 막고 재시도를 제공합니다. 프로필 편집기가 390px 화면 안에 들어오도록 수정했습니다. 진단·경고·엄격 모드 업데이트 문구를 영어·한국어·중국어 간체로 제공합니다.

[#25](https://github.com/cskwork/scopeweaver/pull/25)와 [제공자 안내](../docs/ko/llm-providers.md)를 참고하세요.

### 호환성·전환·한계

**기존 엄격 모드 LAN/Docker 프로필은 다시 연결하기 전에 CIDR을 명시해야 합니다.** 추론 서버의 실제 주소를 **추가 신뢰 CIDR**, `SCOPEWEAVER_LOCAL_LLM_ALLOWED_CIDRS` 또는 기존 `ARTEX_LOCAL_LLM_ALLOWED_CIDRS`에 추가하세요. IPv4 `/32`나 IPv6 `/128`을 권장합니다. Docker에서는 ScopeWeaver 컨테이너 안에서 주소를 확인하세요. `host.docker.internal`이나 `ollama`라는 이름만으로 신뢰하지 않습니다. 같은 컴퓨터의 루프백에는 추가 설정이 필요 없습니다. [전환 안내](../docs/ko/llm-providers.md#네트워크-정책local_llm_only)를 참고하세요.

정책의 기본값은 꺼짐입니다. 이전 릴리스에서 도입한 네이티브 `/api/chat`·키 없는 로컬 프로필·클라우드 제공자 지원을 유지합니다. Anthropic·OpenAI Chat Completions·OpenAI Responses와 기존 설정 경로를 계속 지원하며 의존성과 데이터베이스 스키마는 바꾸지 않습니다.

이 보호 장치는 애플리케이션 제어이며 호스트 전체나 암호학적 외부 전송 차단을 보장하지 않습니다. 대상 도구와 전역 프록시는 네트워크 접근을 유지하며 이미 전송한 트래픽은 되돌릴 수 없습니다. 호스트·원격 Ollama의 클라우드 전달은 별도로 `OLLAMA_NO_CLOUD=1`로 꺼야 하며 ScopeWeaver가 해당 서버의 전달을 제어할 수는 없습니다. GPU 메모리 비율은 추정값으로 정확한 가중치 배치·GPU 용량·모델 상주를 보장하지 않습니다. 문맥 한도 경고는 휴리스틱이며 모든 잘림을 탐지한다고 보장하지 않습니다. 프로토콜 검증으로 자율 작업이나 보안 평가의 품질을 입증하지 않습니다.

### 검증

`d4b93a3`의 병합 전 증거에서 Go 테스트·하위 테스트 1,366개가 통과했고 문서에 명시한 선택 실행 항목 2개는 건너뛰었습니다. 어댑터는 별도로 단위 테스트 7개와 실제 MCP·Pi 시나리오 3개를 확인했습니다. 프런트엔드 테스트 45개와 다시 빌드한 내장 앱의 브라우저 검사 60개가 통과했으며 데스크톱·모바일 EN/KO/zh-Hans를 포함합니다. 집중 race 검사·vet·정적 빌드와 PR 검사 5개도 통과했습니다. 병합 후·릴리스 검사는 별도 단계입니다. [검증 증거와 한계](../docs/ko/VERIFICATION.md)를 참고하세요.

기존 호스트 Ollama 0.32.15와 `qwen3:4b`를 출력 한도 4,096토큰으로 사용해 네이티브 진단·decision/comment 스키마·구조화된 도구 호출 및 결과 재전달·사용량·할당 문맥 32,768토큰을 확인했습니다. 앞선 256·512토큰 시도는 실패했으며 기록을 유지합니다. 모델을 내려받지 않았고, 작업 품질 벤치마크가 아닌 제한된 프로토콜 검사입니다. `TestLiveContextReview`와 유료 제공자 호출은 실행하지 않았습니다.

---

[Compare v0.4.0...v0.5.0](https://github.com/cskwork/scopeweaver/compare/v0.4.0...v0.5.0)
