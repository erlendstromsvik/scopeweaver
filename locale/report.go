package locale

func init() {
	Register("**PoC / evidence:**\n\n```\n%s\n```\n\n", "**PoC / evidence:**\n\n```\n%s\n```\n\n", "**PoC / 증거:**\n\n```\n%s\n```\n\n")
	Register("Unclassified", "Unclassified", "미분류")
	Register("_No vulnerabilities were confirmed._\n\n", "_No vulnerabilities were confirmed._\n\n", "_확인된 취약점이 없습니다._\n\n")
	Register("## Findings\n\n", "## Findings\n\n", "## 발견 사항\n\n")
	Register("- Assets: ", "- Assets: ", "- 자산: ")
	Register("- Confirmed findings: **%d**\n", "- Confirmed findings: **%d**\n", "- 확인된 취약점: **%d**건\n")
	Register("## Summary\n\n", "## Summary\n\n", "## 요약\n\n")
	Register("- **Generated at**: %s\n\n", "- **Generated at**: %s\n\n", "- **생성 시간**: %s\n\n")
	Register("(not specified)", "(not specified)", "(지정되지 않음)")
	Register("- **Task objective**: %s\n", "- **Task objective**: %s\n", "- **작업 목표**: %s\n")
	Register("Untitled task", "Untitled task", "제목 없는 작업")
	Register("# Penetration test report — %s\n\n", "# Penetration test report — %s\n\n", "# 침투 테스트 보고서 — %s\n\n")
	Register("   [Request](evidence/%d/%d/request.http) · [Response](evidence/%d/%d/response.http)\n", "   [Request](evidence/%d/%d/request.http) · [Response](evidence/%d/%d/response.http)\n", "   [요청](evidence/%d/%d/request.http) · [응답](evidence/%d/%d/response.http)\n")
	Register("%d. **Evidence #%d · %s** — `%s %s`, status %d\n", "%d. **Evidence #%d · %s** — `%s %s`, status %d\n", "%d. **증거 #%d · %s** — `%s %s`, 상태 코드 %d\n")
	Register("Evidence has changed; the detailed report needs updating.\n\n", "Evidence has changed; the detailed report needs updating.\n\n", "증거가 변경되어 상세 보고서를 업데이트해야 합니다.\n\n")
	Register("Evidence version: %d; linked items: %d.\n\n", "Evidence version: %d; linked items: %d.\n\n", "증거 버전: %d; 연결된 항목: %d.\n\n")
	Register("\n## Linked traffic evidence\n\n", "\n## Linked traffic evidence\n\n", "\n## 연결된 트래픽 증거\n\n")
	Register("Traffic evidence IDs", "Traffic evidence IDs", "트래픽 증거 ID")
	Register("Traffic evidence count", "Traffic evidence count", "트래픽 증거 수")
	Register("Overview", "Overview", "개요")
	Register("Found at", "Found at", "발견 시간")
	Register("Task", "Task", "작업")
	Register("Status", "Status", "상태")
	Register("Severity", "Severity", "심각도")
	Register("Class", "Class", "분류")
	Register("Name", "Name", "이름")
	Register("## Detailed report\n\n", "## Detailed report\n\n", "## 상세 보고서\n\n")
	Register("## Evidence\n\n```\n%s\n```\n\n", "## Evidence\n\n```\n%s\n```\n\n", "## 증거\n\n```\n%s\n```\n\n")
	Register("## Overview\n\n%s\n\n", "## Overview\n\n%s\n\n", "## 개요\n\n%s\n\n")
	Register("- **Found at**: %s\n", "- **Found at**: %s\n", "- **발견 시간**: %s\n")
	Register("- **Task**: %s\n", "- **Task**: %s\n", "- **작업**: %s\n")
	Register("- **Status**: %s\n", "- **Status**: %s\n", "- **상태**: %s\n")
	Register("- **Severity**: %s\n", "- **Severity**: %s\n", "- **심각도**: %s\n")
	Register("- **Class**: %s\n", "- **Class**: %s\n", "- **분류**: %s\n")
	Register("**Detailed report:**\n\n", "**Detailed report:**\n\n", "**상세 보고서:**\n\n")
	Register("**Evidence:**\n\n```\n%s\n```\n\n", "**Evidence:**\n\n```\n%s\n```\n\n", "**증거:**\n\n```\n%s\n```\n\n")
	Register("- **Found at**: %s\n\n", "- **Found at**: %s\n\n", "- **발견 시간**: %s\n\n")
	Register("## Finding details\n\n", "## Finding details\n\n", "## 발견 사항 상세\n\n")
	Register("_No matching vulnerabilities._\n", "_No matching vulnerabilities._\n", "_일치하는 취약점이 없습니다._\n")
	Register("Low", "Low", "낮음")
	Register("Medium", "Medium", "보통")
	Register("High", "High", "높음")
	Register("Critical", "Critical", "치명적")
	Register("| Severity | Count |\n| --- | --- |\n", "| Severity | Count |\n| --- | --- |\n", "| 심각도 | 수량 |\n| --- | --- |\n")
	Register("- **Total findings**: %d\n\n", "- **Total findings**: %d\n\n", "- **총 발견 수**: %d건\n\n")
	Register("- **Generated at**: %s\n", "- **Generated at**: %s\n", "- **생성 시간**: %s\n")
	Register("# Vulnerability findings report\n\n", "# Vulnerability findings report\n\n", "# 취약점 발견 보고서\n\n")
	Register("Type", "Type", "유형")
	Register("Value", "Value", "값")
	Register("Root domain", "Root domain", "루트 도메인")
	Register("Task IDs", "Task IDs", "작업 ID")
	Register("Last seen", "Last seen", "마지막 관측")
	Register("Source", "Source", "출처")
	Register("Details", "Details", "세부 정보")
	// Coverage report (roadmap E.5a). The last string is the non-compliance
	// disclaimer; "compliance" appears nowhere else in the report.
	Register("# Coverage report\n\n", "# Coverage report\n\n", "# 커버리지 보고서\n\n")
	Register("Generated: %s\n\n", "Generated: %s\n\n", "생성: %s\n\n")
	Register("Verified against %d of %d applicable %s requirements (scope: %s).\n\n", "Verified against %d of %d applicable %s requirements (scope: %s).\n\n", "%d개를 검증했습니다(적용 %d개 중, %s 요구 사항, 범위 %s).\n\n")
	Register("all tests", "all tests", "전체 테스트")
	Register("level 1", "level 1", "레벨 1")
	Register("levels 1–%d", "levels 1–%d", "레벨 1–%d")
	Register("| Standard | In scope | Not applicable | Verified | Pass | Fail | Partial | Inconclusive | Not tested | No result |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n", "| Standard | In scope | Not applicable | Verified | Pass | Fail | Partial | Inconclusive | Not tested | No result |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n", "| 표준 | 범위 내 | 적용 불가 | 검증됨 | 통과 | 실패 | 부분 | 불확실 | 미테스트 | 결과 없음 |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	Register("## Coverage matrix\n\n", "## Coverage matrix\n\n", "## 커버리지 매트릭스\n\n")
	Register("| Requirement | Level | Verdict | Method | Evidence | Findings |\n| --- | --- | --- | --- | --- | --- |\n", "| Requirement | Level | Verdict | Method | Evidence | Findings |\n| --- | --- | --- | --- | --- | --- |\n", "| 요구 사항 | 레벨 | 판정 | 방법 | 증거 | 발견 사항 |\n| --- | --- | --- | --- | --- | --- |\n")
	Register("## Limitations\n\n", "## Limitations\n\n", "## 한계\n\n")
	Register("%d %s requirements were not tested, %d were inconclusive, and %d have no recorded result.\n", "%d %s requirements were not tested, %d were inconclusive, and %d have no recorded result.\n", "%d개의 %s 요구 사항을 테스트하지 않았고, %d개는 불확실하며, %d개는 기록된 결과가 없습니다.\n")
	Register("%d recorded results were outside the selected scope and are not counted.\n", "%d recorded results were outside the selected scope and are not counted.\n", "기록된 결과 %d개는 선택된 범위 밖이므로 계산에 포함되지 않습니다.\n")
	Register("This report states what was verified and how. It is not a certification or a statement of compliance.", "This report states what was verified and how. It is not a certification or a statement of compliance.", "이 보고서는 검증한 내용과 방법을 명시합니다. 인증이나 컴플라이언스 선언이 아닙니다.")
	// Coverage matrix CSV/JSON export (roadmap E.5b). Header cells; the same
	// words are reused by the JSON field labels.
	Register("Standard", "Standard", "표준")
	Register("Requirement", "Requirement", "요구 사항")
	Register("Level", "Level", "레벨")
	Register("Verdict", "Verdict", "판정")
	Register("Method", "Method", "방법")
	Register("Evidence", "Evidence", "증거")
	Register("Findings", "Findings", "발견 사항")
	Register("Reason", "Reason", "사유")
	// Findings by OWASP Top 10 (roadmap E.5c). The "Unclassified" group label is
	// the pre-existing key from report/findings.go; the advisory note says the
	// crosswalk links are not authoritative.
	Register("## Findings by OWASP Top 10\n\n", "## Findings by OWASP Top 10\n\n", "## OWASP Top 10별 발견 사항\n\n")
	Register("ASVS (advisory) lists requirements linked to the finding's CWE through the OpenCRE crosswalk. These links are not authoritative.", "ASVS (advisory) lists requirements linked to the finding's CWE through the OpenCRE crosswalk. These links are not authoritative.", "권고 사항인 ASVS(참고)는 OpenCRE 크로스워크를 통해 이 발견 사항의 CWE와 연결된 요구 사항을 나열합니다. 이 연결은 권위가 없습니다.")
	Register("| Finding | Severity | Status | CWE | ASVS | ASVS (advisory) |\n| --- | --- | --- | --- | --- | --- |\n", "| Finding | Severity | Status | CWE | ASVS | ASVS (advisory) |\n| --- | --- | --- | --- | --- | --- |\n", "| 발견 사항 | 심각도 | 상태 | CWE | ASVS | ASVS (참고) |\n| --- | --- | --- | --- | --- | --- |\n")
	Register("No findings recorded for this task.", "No findings recorded for this task.", "이 작업에 기록된 발견 사항이 없습니다.")
	Register(" (+%d more)", " (+%d more)", " (+%d개 더)")
	// Advisories section (roadmap E.7f). The KEV rows are candidates to verify,
	// never findings; the "latest" NSM date is a raw register date.
	Register("## Advisories\n\n", "## Advisories\n\n", "## 권고 사항\n\n")
	Register("Sources checked: CISA KEV catalog %s (%d entries); NSM vulnerability alerts register (%d alerts, latest %s).\n\n", "Sources checked: CISA KEV catalog %s (%d entries); NSM vulnerability alerts register (%d alerts, latest %s).\n\n", "확인한 출처: CISA KEV 카탈로그 %s(%d개 항목); NSM 취약점 경보 레지스터(%d개 경보, 최신 %s).\n\n")
	Register("### Known exploited vulnerabilities that may apply\n\n", "### Known exploited vulnerabilities that may apply\n\n", "### 적용될 수 있는 알려진 악용 취약점\n\n")
	Register("| Asset | Technology | CVE | Vulnerability | Added |\n| --- | --- | --- | --- | --- |\n", "| Asset | Technology | CVE | Vulnerability | Added |\n| --- | --- | --- | --- | --- |\n", "| 자산 | 기술 | CVE | 취약점 | 추가일 |\n| --- | --- | --- | --- | --- |\n")
	Register("No KEV entries matched the detected technologies of this task's assets.", "No KEV entries matched the detected technologies of this task's assets.", "KEV 항목이 이 작업의 자산에서 감지된 기술과 일치하지 않습니다.")
	Register("These are candidates to verify against the actual version in use, not findings.", "These are candidates to verify against the actual version in use, not findings.", "이 항목은 실제로 사용 중인 버전에 대해 확인이 필요한 후보이며 발견 사항이 아닙니다.")
	Register("### Recent NSM alerts\n\n", "### Recent NSM alerts\n\n", "### 최근 NSM 경보\n\n")
	Register("No NSM alerts in the last 30 days.", "No NSM alerts in the last 30 days.", "지난 30일간의 NSM 경보가 없습니다.")
	RegisterAll(map[string]map[Lang]string{
		"## Advisories\n\n": {
			ZhHans: "## 公告\n\n",
		},
		"Sources checked: CISA KEV catalog %s (%d entries); NSM vulnerability alerts register (%d alerts, latest %s).\n\n": {
			ZhHans: "已检查来源：CISA KEV 目录 %s（%d 个条目）；NSM 漏洞警报登记册（%d 条警报，最新为 %s）。\n\n",
		},
		"### Known exploited vulnerabilities that may apply\n\n": {
			ZhHans: "### 可能适用的已知被利用漏洞\n\n",
		},
		"| Asset | Technology | CVE | Vulnerability | Added |\n| --- | --- | --- | --- | --- |\n": {
			ZhHans: "| 资产 | 技术 | CVE | 漏洞 | 添加日期 |\n| --- | --- | --- | --- | --- |\n",
		},
		"No KEV entries matched the detected technologies of this task's assets.": {
			ZhHans: "没有 KEV 条目与本任务资产检测到的技术相匹配。",
		},
		"These are candidates to verify against the actual version in use, not findings.": {
			ZhHans: "这些是需要与实际使用版本进行核实的候选项，并非发现结果。",
		},
		"### Recent NSM alerts\n\n": {
			ZhHans: "### 近期 NSM 警报\n\n",
		},
		"No NSM alerts in the last 30 days.": {
			ZhHans: "最近 30 天内没有 NSM 警报。",
		},
	})
}
