package locale

import "testing"

func TestChatShutdownTranslations(t *testing.T) {
	const key = "server is shutting down"
	for lang, want := range map[Lang]string{En: key, Ko: "서버를 종료하는 중입니다", ZhHans: "服务器正在关闭"} {
		if got, ok := Raw(lang, key); !ok || got != want {
			t.Errorf("%s: got %q, want %q", lang, got, want)
		}
	}
	if err := ValidateTranslations([]string{key}); err != nil {
		t.Fatal(err)
	}
}
