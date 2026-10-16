package server

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

// S11 (API): existing response keys/values survive the additive reasoning field.
func TestTokenUsageReasoningAPI(t *testing.T) {
	m := backportTestManager(t)
	s := &Server{m: m}
	if _, err := m.pg.Exec(`DELETE FROM llm_usage`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = m.pg.Exec(`DELETE FROM llm_usage`) })
	if err := m.pg.InsertLLMUsage(&db.LLMUsage{TaskID: "reasoning-api", ProfileName: "profile", Model: "model", Worker: "judge", InputTokens: 11, OutputTokens: 7, CacheRead: 3, CacheWrite: 2, ReasoningTokens: 4, Status: "error"}); err != nil {
		t.Fatal(err)
	}
	day := time.Now().UTC().Format("2006-01-02")
	for _, tc := range []struct {
		path    string
		handler func(*httptest.ResponseRecorder)
		want    string
	}{
		{"/api/llm/records/by-model?task=reasoning-api", func(w *httptest.ResponseRecorder) {
			s.pgTokenByModel(w, httptest.NewRequest("GET", "/api/llm/records/by-model?task=reasoning-api", nil))
		}, `{"models":[{"model":"model","calls":1,"input_tokens":11,"output_tokens":7,"cache_read_tokens":3,"cache_write_tokens":2,"reasoning_tokens":4}]}`},
		{"/api/tokens/usage", func(w *httptest.ResponseRecorder) {
			s.pgUsageStats(w, httptest.NewRequest("GET", "/api/tokens/usage", nil))
		}, `{"by_profile":[{"profile_name":"profile","calls":1,"tasks":1,"input_tokens":11,"output_tokens":7,"cache_read_tokens":3,"cache_write_tokens":2,"reasoning_tokens":4}],"daily":[{"profile_name":"profile","date":"` + day + `","input_tokens":11,"output_tokens":7,"cache_read_tokens":3,"reasoning_tokens":4}]}`},
		{"/api/intercept/judge/usage?days=1", func(w *httptest.ResponseRecorder) {
			s.interceptJudgeUsage(w, httptest.NewRequest("GET", "/api/intercept/judge/usage?days=1", nil))
		}, `{"calls":1,"input_tokens":11,"output_tokens":7,"cache_read_tokens":3,"cache_write_tokens":2,"reasoning_tokens":4,"daily":[{"date":"` + day + `","calls":1,"input_tokens":11,"output_tokens":7,"reasoning_tokens":4}]}`},
	} {
		t.Run(tc.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			tc.handler(w)
			if w.Code != 200 {
				t.Fatalf("status=%d: %s", w.Code, w.Body.String())
			}
			var got, want any
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("response=%s want %s", w.Body.String(), tc.want)
			}
		})
	}
}
