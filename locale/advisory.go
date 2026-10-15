package locale

func init() {
	RegisterAll(map[string]map[Lang]string{
		"parse CISA KEV catalog: %v": {
			En:     "parse CISA KEV catalog: %v",
			Ko:     "CISA KEV 카탈로그 파싱 오류: %v",
			ZhHans: "解析 CISA KEV 目录：%v",
		},
		"CISA KEV count field %d does not match %d entries": {
			En:     "CISA KEV count field %d does not match %d entries",
			Ko:     "CISA KEV count 필드 %d이(가) %d개 항목과 일치하지 않습니다",
			ZhHans: "CISA KEV count 字段 %d 与 %d 个条目不匹配",
		},
		"CISA KEV entry missing cveID (product %q)": {
			En:     "CISA KEV entry missing cveID (product %q)",
			Ko:     "CISA KEV 항목에 cveID이(가) 없습니다(제품 %q)",
			ZhHans: "CISA KEV 条目缺少 cveID（产品 %q）",
		},
		"parse CISA KEV dateAdded %q for %s: %v": {
			En:     "parse CISA KEV dateAdded %q for %s: %v",
			Ko:     "%[2]s의 CISA KEV dateAdded %[1]q 파싱 오류: %[3]v",
			ZhHans: "解析 %[2]s 的 CISA KEV dateAdded %[1]q：%[3]v",
		},
	})
}
