package locale

import "testing"

func TestTranscriptRecoveryDiagnosticLanguages(t *testing.T) {
	const key = "transcript path is not a directory: %s"
	const path = "C:/사용자/用户/100%/transcripts"
	for _, tc := range []struct {
		lang Lang
		want string
	}{
		{En, "transcript path is not a directory: " + path},
		{Ko, "대화 기록 경로가 디렉터리가 아닙니다: " + path},
		{ZhHans, "对话记录路径不是目录：" + path},
	} {
		t.Run(string(tc.lang), func(t *testing.T) {
			if _, ok := Raw(tc.lang, key); !ok {
				t.Fatal("missing transcript recovery translation")
			}
			if got := Text(tc.lang, key, path); got != tc.want {
				t.Fatalf("diagnostic = %q; want %q", got, tc.want)
			}
		})
	}
}
