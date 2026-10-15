package locale

func init() {
	RegisterAll(map[string]map[Lang]string{
		// Blueprint test profiles (roadmap E.4). The first is the validator's
		// unknown-profile rejection; the second is the reason seeded on a
		// not_tested control result for a requirement automation cannot settle.
		"standards[%d]: unknown profile %q; allowed: %s": {
			En:     "standards[%d]: unknown profile %q; allowed: %s",
			Ko:     "standards[%d]: 알 수 없는 프로필 %q; 허용되는 프로필: %s",
			ZhHans: "standards[%d]：未知配置文件 %q；允许的配置文件：%s",
		},
		"Requires documentation review; not checked by automation.": {
			En:     "Requires documentation review; not checked by automation.",
			Ko:     "문서 검토가 필요하며 자동화로 점검되지 않습니다.",
			ZhHans: "需要文档审查；自动化不会检查。",
		},
	})
}
