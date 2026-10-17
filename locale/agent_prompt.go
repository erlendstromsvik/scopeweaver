package locale

func init() {
	RegisterAll(map[string]map[Lang]string{
		"[prompt] agent=%q: template parsing failed; using role default": {
			En:     "[prompt] agent=%q: template parsing failed; using role default",
			Ko:     "[prompt] agent=%q: 템플릿 구문 분석 실패; 역할 기본 프롬프트 사용",
			ZhHans: "[prompt] agent=%q：模板解析失败；使用该角色的默认提示词",
		},
		"[prompt] agent=%q: template execution failed; using role default": {
			En:     "[prompt] agent=%q: template execution failed; using role default",
			Ko:     "[prompt] agent=%q: 템플릿 실행 실패; 역할 기본 프롬프트 사용",
			ZhHans: "[prompt] agent=%q：模板执行失败；使用该角色的默认提示词",
		},
	})
}
