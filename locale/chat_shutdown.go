package locale

func init() {
	RegisterAll(map[string]map[Lang]string{
		"server is shutting down": {En: "server is shutting down", Ko: "서버를 종료하는 중입니다", ZhHans: "服务器正在关闭"},
	})
}
