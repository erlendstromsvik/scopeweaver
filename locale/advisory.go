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

		"parse NSM RSS feed: %v": {
			En:     "parse NSM RSS feed: %v",
			Ko:     "NSM RSS 피드 파싱 오류: %v",
			ZhHans: "解析 NSM RSS 源：%v",
		},
		"NSM RSS feed has no channel": {
			En:     "NSM RSS feed has no channel",
			Ko:     "NSM RSS 피드에 channel이 없습니다",
			ZhHans: "NSM RSS 源没有 channel",
		},
		"NSM RSS item missing link (title %q)": {
			En:     "NSM RSS item missing link (title %q)",
			Ko:     "NSM RSS 항목에 link이 없습니다(제목 %q)",
			ZhHans: "NSM RSS 条目缺少 link（标题 %q）",
		},
		"parse NSM RSS pubDate %q for item %q: %v": {
			En:     "parse NSM RSS pubDate %q for item %q: %v",
			Ko:     "%[2]q 항목의 NSM RSS pubDate %[1]q 파싱 오류: %[3]v",
			ZhHans: "解析 %[2]q 条目的 NSM RSS pubDate %[1]q：%[3]v",
		},

		"register line %d: %v": {
			En:     "register line %d: %v",
			Ko:     "레지스터 %[1]d행 오류: %[2]v",
			ZhHans: "寄存器第 %[1]d 行：%[2]v",
		},
		"register line %d: missing id": {
			En:     "register line %d: missing id",
			Ko:     "레지스터 %d행: id이(가) 없습니다",
			ZhHans: "寄存器第 %d 行：缺少 id",
		},
		"register line %d: duplicate id %s": {
			En:     "register line %d: duplicate id %s",
			Ko:     "레지스터 %[1]d행: 중복된 id %[2]s",
			ZhHans: "寄存器第 %[1]d 行：重复的 id %[2]s",
		},
		"read register: %v": {
			En:     "read register: %v",
			Ko:     "레지스터 읽기 오류: %v",
			ZhHans: "读取寄存器：%v",
		},
		"append register entry %s: %v": {
			En:     "append register entry %s: %v",
			Ko:     "%[1]s 레지스터 항목 추가 오류: %[2]v",
			ZhHans: "追加寄存器条目 %[1]s：%[2]v",
		},
	})
}
