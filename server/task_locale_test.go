package server

import (
	"context"
	"encoding/json"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
	"github.com/Autumn-27/artex/locale"
	"strconv"
	"strings"
	"testing"
)

func TestSpawnTaskInheritsRunOrParentLanguage(t *testing.T) {
	dbtest.DSN(t) // skip unless ARTEX_TEST_DSN points at a throwaway DB
	previousDefault := locale.ServerDefault()
	defer locale.SetServerDefault(previousDefault)
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("isolated PostgreSQL unavailable: %v", err)
	}
	defer m.Close()
	parent, err := m.CreateTaskWithOptions("locale parent", "no network target", db.TaskCreateOptions{Language: locale.Ko})
	if err != nil {
		t.Fatal(err)
	}
	parentID, _ := strconv.ParseInt(parent.ID, 10, 64)
	defer m.pg.DeleteTask(parentID)
	s := newAdmissionTestServer(m, func(*Task) bool { return false })
	for _, tc := range []struct {
		name string
		ctx  context.Context
		want locale.Lang
	}{{"parent", context.Background(), locale.Ko}, {"run", locale.WithLang(context.Background(), locale.En), locale.En}, {"chinese", locale.WithLang(context.Background(), locale.ZhHans), locale.ZhHans}} {
		input, _ := json.Marshal(map[string]any{"description": "locale child " + tc.name, "goal": "no network target", "parent_ref": parent.ID})
		result, err := s.toolSpawnTask().Call(tc.ctx, input, nil)
		if err != nil || result.IsError {
			t.Fatalf("spawn %s: %v %s", tc.name, err, result.Flatten())
		}
		childID := strings.TrimPrefix(result.Flatten(), "task created: ")
		id, parseErr := strconv.ParseInt(childID, 10, 64)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		defer m.pg.DeleteTask(id)
		if got := taskLanguage(m.pg, childID); got != tc.want {
			t.Fatalf("%s language=%s want %s", tc.name, got, tc.want)
		}
	}
}
