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
		"parse NSM archive page: %v": {
			En:     "parse NSM archive page: %v",
			Ko:     "NSM 아카이브 페이지 파싱 오류: %v",
			ZhHans: "解析 NSM 存档页面：%v",
		},
		"NSM archive article missing link (title %q)": {
			En:     "NSM archive article missing link (title %q)",
			Ko:     "NSM 아카이브 항목에 link이 없습니다(제목 %q)",
			ZhHans: "NSM 存档条目缺少 link（标题 %q）",
		},
		"NSM archive article missing headline (link %s)": {
			En:     "NSM archive article missing headline (link %s)",
			Ko:     "NSM 아카이브 항목에 headline이 없습니다(link %s)",
			ZhHans: "NSM 存档条目缺少 headline（链接 %s）",
		},
		"parse NSM archive date for article %q: %v": {
			En:     "parse NSM archive date for article %q: %v",
			Ko:     "%[1]q 항목의 NSM 아카이브 날짜 파싱 오류: %[2]v",
			ZhHans: "解析 %[1]q 条目的 NSM 存档日期：%[2]v",
		},
		"unrecognised date %q": {
			En:     "unrecognised date %q",
			Ko:     "인식할 수 없는 날짜 %q",
			ZhHans: "无法识别的日期 %q",
		},
		"unknown Norwegian month %q": {
			En:     "unknown Norwegian month %q",
			Ko:     "알 수 없는 노르웨이 월 %q",
			ZhHans: "未知的挪威月份 %q",
		},
		"unrecognised day %q": {
			En:     "unrecognised day %q",
			Ko:     "인식할 수 없는 일 %q",
			ZhHans: "无法识别的日期数字 %q",
		},
		"unrecognised year %q": {
			En:     "unrecognised year %q",
			Ko:     "인식할 수 없는 연도 %q",
			ZhHans: "无法识别的年份 %q",
		},
		"no alerts found on the NSM archive page; the page format may have changed": {
			En:     "no alerts found on the NSM archive page; the page format may have changed",
			Ko:     "NSM 아카이브 페이지에서 경고를 찾지 못했습니다; 페이지 형식이 변경되었을 수 있습니다",
			ZhHans: "在 NSM 存档页面未找到警报；页面格式可能已更改",
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

		"fetch NSM RSS feed: %w": {
			En:     "fetch NSM RSS feed: %w",
			Ko:     "NSM RSS 피드 가져오기 오류: %w",
			ZhHans: "获取 NSM RSS 源：%w",
		},
		"NSM RSS feed returned HTTP %d": {
			En:     "NSM RSS feed returned HTTP %d",
			Ko:     "NSM RSS 피드가 HTTP %d을(를) 반환했습니다",
			ZhHans: "NSM RSS 源返回 HTTP %d",
		},
		"fetch CISA KEV catalog: %w": {
			En:     "fetch CISA KEV catalog: %w",
			Ko:     "CISA KEV 카탈로그 가져오기 오류: %w",
			ZhHans: "获取 CISA KEV 目录：%w",
		},
		"CISA KEV catalog returned HTTP %d": {
			En:     "CISA KEV catalog returned HTTP %d",
			Ko:     "CISA KEV 카탈로그가 HTTP %d을(를) 반환했습니다",
			ZhHans: "CISA KEV 目录返回 HTTP %d",
		},
		"read CISA KEV snapshot %s: %w": {
			En:     "read CISA KEV snapshot %s: %w",
			Ko:     "CISA KEV 스냅샷 %[1]s 읽기 오류: %[2]w",
			ZhHans: "读取 CISA KEV 快照 %[1]s：%[2]w",
		},
		"write CISA KEV snapshot %s: %w": {
			En:     "write CISA KEV snapshot %s: %w",
			Ko:     "CISA KEV 스냅샷 %[1]s 쓰기 오류: %[2]w",
			ZhHans: "写入 CISA KEV 快照 %[1]s：%[2]w",
		},
	})
}
