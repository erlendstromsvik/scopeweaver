package locale

func init() {
	RegisterAll(map[string]map[Lang]string{
		"transcript path is not a directory: %s": {
			En:     "transcript path is not a directory: %s",
			Ko:     "대화 기록 경로가 디렉터리가 아닙니다: %s",
			ZhHans: "对话记录路径不是目录：%s",
		},
	})
}
