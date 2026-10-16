package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
	"github.com/google/uuid"
)

func TestSliceCDeepSeekProbeMetered(t *testing.T) {
	dbtest.DSN(t)
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	old, err := m.pg.ActiveProfile()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if old != nil {
			m.pg.SetActiveProfile(old.ID)
		}
	}()
	t.Setenv("SCOPEWEAVER_LOCAL_LLM_ONLY", "false")
	saved, existed, err := m.pg.GetSetting(settingLocalLLMOnly)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if existed {
			m.pg.SetSetting(settingLocalLLMOnly, saved)
		} else {
			m.pg.Exec(`DELETE FROM settings WHERE key=$1`, settingLocalLLMOnly)
		}
	}()
	if err = m.pg.SetBool(settingLocalLLMOnly, false); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, tail, status string
		wantOK, hang       bool
		wantInput          int
	}{
		{"http-error", "", "error", false, false, 0},
		{"normal", "data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":100,\"output_tokens\":20}}\n\ndata: {\"type\":\"message_stop\"}\n\n", "ok", true, false, 100},
		{"late-error", "data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":100,\"output_tokens\":20}}\n\ndata: {\"type\":\"error\",\"error\":{\"message\":\"fixture failed\"}}\n\n", "error", true, false, 100},
		{"unknown-usage", "", "ok", true, false, 0},
		{"late-timeout", "", "error", true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			profile := "search-probe-" + uuid.NewString()
			defer m.pg.Exec(`DELETE FROM llm_usage WHERE profile_name=$1`, profile)
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.name == "http-error" {
					http.Error(w, "fixture failed", 503)
					return
				}
				fmt.Fprint(w, "data: {\"type\":\"content_block_start\",\"content_block\":{\"type\":\"web_search_tool_result\",\"content\":[{\"url\":\"https://a.example\"},{\"url\":\"https://b.example\"},{\"url\":\"https://c.example\"}]}}\n\n"+tc.tail)
				w.(http.Flusher).Flush()
				if tc.hang {
					<-r.Context().Done()
				}
			}))
			defer fixture.Close()
			id, e := m.pg.SaveProfile(&db.LLMProfile{Name: profile, BaseURL: fixture.URL, APIKey: "fixture", Model: "probe-model", Format: "anthropic"})
			if e != nil {
				t.Fatal(e)
			}
			defer m.pg.DeleteProfile(id)
			if e = m.pg.SetActiveProfile(id); e != nil {
				t.Fatal(e)
			}
			s := &Server{m: m}
			w := httptest.NewRecorder()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.hang {
				time.AfterFunc(50*time.Millisecond, cancel)
			}
			req := httptest.NewRequest("POST", "/api/settings/web-search/test", bytes.NewBufferString(`{"web_search_backend":"deepseek"}`)).WithContext(ctx)
			s.testWebSearch(w, req)
			var out struct {
				OK    bool `json:"ok"`
				Count int  `json:"count"`
			}
			if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
				t.Fatal(e)
			}
			wantCount := 3
			if !tc.wantOK {
				wantCount = 0
			}
			if w.Code != 200 || out.OK != tc.wantOK || out.Count != wantCount {
				t.Fatalf("probe: %d %s", w.Code, w.Body.String())
			}
			var count, input int
			var status, model string
			e = m.pg.QueryRow(`SELECT COUNT(*),COALESCE(SUM(input_tokens),0),COALESCE(MAX(status),''),COALESCE(MAX(model),'') FROM llm_usage WHERE profile_name=$1 AND worker='web_search'`, profile).Scan(&count, &input, &status, &model)
			if e != nil {
				t.Fatal(e)
			}
			if count != 1 || input != tc.wantInput || status != tc.status || model != "probe-model" {
				t.Fatalf("row %d %d %s %s", count, input, status, model)
			}
		})
	}
}
