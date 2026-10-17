package db

import (
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/chatstate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// Each connection resolves only the test-owned schema, never the public tables.
func chatUpgradeFixture(t *testing.T) *DB {
	t.Helper()
	cfg, err := pgx.ParseConfig(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(cfg.Database, "scopeweaver_test_") {
		t.Fatal("chat upgrade requires a disposable scopeweaver_test_* database")
	}
	admin := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { admin.Close() })
	schema := pgx.Identifier{"chat_upgrade_" + strings.ReplaceAll(chatID(), "-", "")}.Sanitize()
	if _, err := admin.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
	})
	cfg.RuntimeParams["search_path"] = schema
	d := &DB{stdlib.OpenDB(*cfg)}
	t.Cleanup(func() { d.Close() })
	baseline, err := os.ReadFile("testdata/chat_upgrade_6366429.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(t.Context(), string(baseline)); err != nil {
		t.Fatalf("baseline schema: %v", err)
	}
	if err := d.EnsureLLMUsageTable(); err != nil {
		t.Fatal(err)
	}
	return d
}

func upgradeChatSchema(t *testing.T, d *DB) {
	t.Helper()
	for pass := 1; pass <= 2; pass++ {
		if err := withSchemaMigrationLock(t.Context(), d.DB, func(conn *sql.Conn) error {
			return applySchemaWithRetry(t.Context(), conn, time.Sleep)
		}); err != nil {
			t.Fatalf("upgrade pass %d: %v", pass, err)
		}
	}
}

func TestChatSchemaUpgradePreservesLegacyHistoryAndBilling(t *testing.T) {
	d := chatUpgradeFixture(t)
	const content = "한글 history\n{\"raw\":\"bytes\"}"
	if _, err := d.Exec(`INSERT INTO explorations(id,description,goal) VALUES(1,'legacy task','offline fixture');
INSERT INTO tasks(id,description,goal,exploration_id) VALUES(1,'legacy task','offline fixture',1);
INSERT INTO conversations(id,agent_key,title) VALUES(1,'pentest','legacy conversation');
INSERT INTO activity(exploration_id,worker,kind,summary,detail,main_seg) VALUES(1,'mainagent','user','legacy','original',NULL);
INSERT INTO conversation_activities(conversation_id,worker,kind,summary,detail) VALUES(1,'pentest','user','legacy','original');
INSERT INTO conversation_activities(conversation_id,worker,kind,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens) VALUES(1,'pentest','result',11,7,3,2);
INSERT INTO llm_usage(task_id,exploration_id,worker,model,input_tokens,output_tokens,cache_read,cache_write,reasoning_tokens,status) VALUES('1',1,'mainagent','legacy-model',11,7,3,2,4,'ok');`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"activity", "conversation_activities"} {
		if _, err := d.Exec("UPDATE "+table+" SET detail=$1 WHERE kind='user'", content); err != nil {
			t.Fatal(err)
		}
	}
	upgradeChatSchema(t, d)
	for _, ref := range []chatstate.SessionRef{{TaskID: 1, ExplorationID: 1}, {ConversationID: 1}} {
		rows, err := d.ChatLegacyTimeline(t.Context(), ref)
		if err != nil || len(rows) == 0 || rows[0].Detail != content || rows[0].RunID != "" {
			t.Fatalf("legacy history changed for %+v: %+v, %v", ref, rows, err)
		}
	}
	page, _, err := d.Exploration(1).ActivityPage(ActivitySessionFilter{Main: true, MainSeg: new(int)}, 0, 20)
	if err != nil || len(page) != 1 || page[0].MainSeg != nil || page[0].ActiveAttempt != nil {
		t.Fatalf("legacy NULL segment/attempt no longer readable: %+v, %v", page, err)
	}
	conversation, _, err := d.ConvActivityList(1, 0, 20)
	if err != nil || len(conversation) != 2 {
		t.Fatalf("legacy conversation hidden: %+v, %v", conversation, err)
	}
	tx, err := d.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	usage, err := chatConversationUsageTx(t.Context(), tx, 1)
	if err != nil || usage.InputTokens != 11 || usage.OutputTokens != 7 || usage.CacheReadTokens != 3 || usage.CacheWriteTokens != 2 {
		t.Fatalf("conversation billing changed: %+v, %v", usage, err)
	}
	stats, err := d.TokenByModel("1")
	if err != nil || len(stats) != 1 || stats[0].InputTokens != 11 || stats[0].OutputTokens != 7 || stats[0].ReasoningTokens != 4 || stats[0].CacheReadTokens != 3 || stats[0].CacheWriteTokens != 2 {
		t.Fatalf("usage ledger changed: %+v, %v", stats, err)
	}
	var eagerGraphs int
	if err := d.QueryRow(`SELECT count(*) FROM conversations WHERE exploration_id IS NOT NULL`).Scan(&eagerGraphs); err != nil || eagerGraphs != 0 {
		t.Fatalf("upgrade eagerly rewrote conversation ownership: %d, %v", eagerGraphs, err)
	}
}

func TestChatUpgradeRestoresSerializedV3Archive(t *testing.T) {
	d := chatUpgradeFixture(t)
	upgradeChatSchema(t, d)
	raw, err := os.ReadFile("testdata/chat_archive_v3.json")
	if err != nil {
		t.Fatal(err)
	}
	var snapshot TaskArchiveSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.FormatVersion != 3 || len(snapshot.Tables["chat_control_gates"]) != 0 {
		t.Fatal("fixture must retain its pre-chat-control v3 shape")
	}
	if _, err := d.Exec(`INSERT INTO explorations(id,description,goal) VALUES(1,'','');
INSERT INTO tasks(id,description,goal,exploration_id,deleted_at,archived_at) VALUES(1,'','',1,now(),now());
INSERT INTO task_archives(id,task_id,state,format_version) VALUES(1,1,'restoring',3);`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.RestoreTaskArchive(1, &snapshot, 0); err != nil {
		t.Fatalf("restore serialized v3 archive: %v", err)
	}
	task, err := d.GetTask(1)
	if err != nil || task == nil || task.Name != "v3 archive" || !task.Paused {
		t.Fatalf("restored task: %+v, %v", task, err)
	}
	history, err := d.ChatLegacyTimeline(t.Context(), chatstate.SessionRef{TaskID: 1, ExplorationID: 1})
	if err != nil || len(history) != 1 || history[0].Detail != "v3 original history\n한글" {
		t.Fatalf("v3 historical bytes changed: %+v, %v", history, err)
	}
	stats, err := d.TokenByModel("1")
	if err != nil || len(stats) != 1 || stats[0].InputTokens != 11 || stats[0].OutputTokens != 7 || stats[0].ReasoningTokens != 4 {
		t.Fatalf("v3 billing changed: %+v, %v", stats, err)
	}
	var revision int64
	var source, answer, context string
	if err := d.QueryRow(`SELECT history_revision,source_run_id,answer,context_info::text FROM side_question_requests WHERE id='v3-question'`).Scan(&revision, &source, &answer, &context); err != nil || revision != 0 || source != "" || answer != "v3 answer" || context != "{}" {
		t.Fatalf("v3 side question defaults/history: revision=%d source=%q answer=%q context=%q, %v", revision, source, answer, context, err)
	}
	if err := d.CompleteTaskArchiveRestore(1); err != nil {
		t.Fatal(err)
	}
}
