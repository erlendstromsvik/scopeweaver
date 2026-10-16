package locale

func init() {
	RegisterAll(map[string]map[Lang]string{
		"Web search is unavailable while local_llm_only is enabled":                   {En: "Web search is unavailable while local_llm_only is enabled", Ko: "local_llm_only가 켜져 있을 때는 웹 검색을 사용할 수 없습니다", ZhHans: "启用 local_llm_only 时无法使用网页搜索"},
		"Update checks and downloads are unavailable while local_llm_only is enabled": {En: "Update checks and downloads are unavailable while local_llm_only is enabled", Ko: "local_llm_only가 켜져 있을 때는 업데이트 확인과 다운로드를 사용할 수 없습니다", ZhHans: "启用 local_llm_only 时无法检查或下载更新"},
		"local_llm_only: redirects are not allowed":                                   {En: "local_llm_only: redirects are not allowed", Ko: "local_llm_only: 리디렉션은 허용되지 않습니다", ZhHans: "local_llm_only：不允许重定向"},
		"local_llm_only: LLM destination %s (%s) is not loopback or allowlisted":      {En: "local_llm_only: LLM destination %s (%s) is not loopback or allowlisted", Ko: "local_llm_only: LLM 대상 %s (%s)은 루프백 주소 또는 허용 목록에 없습니다", ZhHans: "local_llm_only：LLM 目标 %s (%s) 不是回环地址且不在允许列表中"},
		"Native Ollama prompt token count is near the configured context limit (model %s, prompt_eval_count %d, num_ctx %d); this heuristic does not prove truncation": {En: "Native Ollama prompt token count is near the configured context limit (model %s, prompt_eval_count %d, num_ctx %d); this heuristic does not prove truncation", Ko: "네이티브 Ollama 프롬프트 토큰 수가 설정된 컨텍스트 한도에 가깝습니다(모델 %s, prompt_eval_count %d, num_ctx %d). 이 추정으로 잘림 여부를 입증할 수는 없습니다", ZhHans: "原生 Ollama 提示词令牌数接近配置的上下文上限（模型 %s，prompt_eval_count %d，num_ctx %d）；此启发式判断不能证明发生了截断"},
	})
}
