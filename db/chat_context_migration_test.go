package db

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/chatstate"
)

// The old eight-table schema's exact current-observation shape.
const legacyChatContextSchema = `CREATE TABLE chat_context_snapshots (
 session_key TEXT PRIMARY KEY REFERENCES chat_control_sessions(session_key) ON DELETE CASCADE,
 run_id TEXT NOT NULL DEFAULT '',
 history_revision BIGINT NOT NULL,
 sequence BIGINT NOT NULL,
 view JSONB NOT NULL,
 request JSONB NOT NULL,
 profile_identity TEXT NOT NULL DEFAULT ''
);`

func legacyChatContextFixture(t *testing.T) (*DB, chatstate.SessionRef, chatstate.ContextView) {
	t.Helper()
	d := chatUpgradeFixture(t)
	upgradeChatSchema(t, d)
	if _, err := d.Exec(`INSERT INTO conversations(id,agent_key,title) VALUES(1,'mainagent','snapshot migration')`); err != nil {
		t.Fatal(err)
	}
	ref := chatstate.SessionRef{ConversationID: 1}
	if _, err := d.ChatControlState(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	view := chatContextSnapshotFixture()
	view.RunID = "legacy-run"
	insertLegacyChatContext(t, d, ref, view)
	return d, ref, view
}

func insertLegacyChatContext(t *testing.T, d *DB, ref chatstate.SessionRef, view chatstate.ContextView) {
	t.Helper()
	if _, err := d.Exec(legacyChatContextSchema); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	request, err := chatContextRequestJSON(view)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO chat_context_snapshots(session_key,run_id,history_revision,sequence,view,request,profile_identity) VALUES($1,$2,$3,$4,$5,$6,$7)`, ref.Key(), view.RunID, view.HistoryRevision, view.Sequence, body, request, view.ProfileIdentity); err != nil {
		t.Fatal(err)
	}
}

func TestChatContextSchemaSevenTablesAndSnapshotMigration(t *testing.T) {
	d, ref, want := legacyChatContextFixture(t)
	upgradeChatSchema(t, d)
	var count int
	if err := d.QueryRow(`SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name LIKE 'chat\_%' ESCAPE '\'`).Scan(&count); err != nil || count != 7 {
		t.Fatal("schema must contain exactly seven chat tables", count, err)
	}
	var retired bool
	if err := d.QueryRow(`SELECT to_regclass('chat_context_snapshots') IS NULL`).Scan(&retired); err != nil || !retired {
		t.Fatal("old table was not retired", retired, err)
	}
	got, err := d.ChatContext(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	assertChatContextSnapshot(t, got, want)
	// A partial upgrade with an identical destination is also safe to resume.
	insertLegacyChatContext(t, d, ref, want)
	upgradeChatSchema(t, d)
	got, err = d.ChatContext(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}
	assertChatContextSnapshot(t, got, want)
}

func TestChatContextSchemaMigrationConflictPreservesSnapshots(t *testing.T) {
	d, ref, _ := legacyChatContextFixture(t)
	if _, err := d.Exec(`UPDATE chat_control_sessions SET context_snapshot='{"conflicting":"preserve me"}' WHERE session_key=$1`, ref.Key()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ExecContext(t.Context(), schemaSQL); err == nil || !strings.Contains(err.Error(), "conflicting payload") {
		t.Fatalf("conflicting migration did not fail explicitly: %v", err)
	}
	var sourceRows int
	if err := d.QueryRow(`SELECT count(*) FROM chat_context_snapshots WHERE session_key=$1`, ref.Key()).Scan(&sourceRows); err != nil || sourceRows != 1 {
		t.Fatal("conflict lost original snapshot", sourceRows, err)
	}
	var retained bool
	if err := d.QueryRow(`SELECT context_snapshot='{"conflicting":"preserve me"}'::jsonb FROM chat_control_sessions WHERE session_key=$1`, ref.Key()).Scan(&retained); err != nil || !retained {
		t.Fatal("conflict overwrote destination", retained, err)
	}
}

func TestChatControlsArchiveLegacySnapshotFailures(t *testing.T) {
	d, ref := controlFixture(t)
	view := chatContextSnapshotFixture()
	if err := d.SaveChatContext(t.Context(), ref, "", view); err != nil {
		t.Fatal(err)
	}
	var body []byte
	if err := d.QueryRow(`SELECT context_snapshot FROM chat_control_sessions WHERE session_key=$1`, ref.Key()).Scan(&body); err != nil {
		t.Fatal(err)
	}
	var row map[string]json.RawMessage
	if err := json.Unmarshal(body, &row); err != nil {
		t.Fatal(err)
	}
	row["session_key"], _ = json.Marshal(ref.Key())
	matching, err := json.Marshal([]map[string]json.RawMessage{row})
	if err != nil {
		t.Fatal(err)
	}
	row["sequence"] = json.RawMessage(`999`)
	conflicting, err := json.Marshal([]map[string]json.RawMessage{row})
	if err != nil {
		t.Fatal(err)
	}
	row["session_key"] = json.RawMessage(`"missing"`)
	missing, err := json.Marshal([]map[string]json.RawMessage{row})
	if err != nil {
		t.Fatal(err)
	}
	row["session_key"], _ = json.Marshal(ref.Key())
	delete(row, "request")
	incomplete, err := json.Marshal([]map[string]json.RawMessage{row})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"foreign-matching", string(matching), true},
		{"malformed", `[{`, true},
		{"incomplete", string(incomplete), true},
		{"missing-session", string(missing), true},
		{"conflicting", string(conflicting), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := d.BeginTx(t.Context(), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if err := RestoreChatControls(t.Context(), tx, map[string]json.RawMessage{"chat_context_snapshots": json.RawMessage(tc.body)}); (err != nil) != tc.wantErr {
				t.Fatalf("old snapshot import error = %v, want error = %v", err, tc.wantErr)
			}
		})
	}
	var after []byte
	if err := d.QueryRow(`SELECT context_snapshot FROM chat_control_sessions WHERE session_key=$1`, ref.Key()).Scan(&after); err != nil || string(body) != string(after) {
		t.Fatal("failed archive import changed the current snapshot", string(after), err)
	}
}

func TestChatControlsArchiveSnapshotValidation(t *testing.T) {
	const valid = `{"run_id":"","history_revision":0,"sequence":0,"view":{"history_revision":"0","sequence":"0","profile_id":"0","categories":null},"request":{},"profile_identity":""}`
	for _, tc := range []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"empty-run-profile-and-optional-null", valid, false},
		{"invalid-history", strings.Replace(valid, `"history_revision":0`, `"history_revision":"invalid"`, 1), true},
		{"fractional-sequence", strings.Replace(valid, `"sequence":0`, `"sequence":0.5`, 1), true},
		{"incomplete", strings.Replace(valid, `"request":{},`, "", 1), true},
		{"null-request", strings.Replace(valid, `"request":{}`, `"request":null`, 1), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateArchivedChatContextSnapshot(json.RawMessage(tc.body)); (err != nil) != tc.wantErr {
				t.Fatalf("snapshot validation error = %v, want error = %v", err, tc.wantErr)
			}
		})
	}
}

func TestChatControlsArchiveRejectsInvalidInlineSnapshot(t *testing.T) {
	d, _ := controlFixture(t)
	for _, snapshot := range []string{
		`{"run_id":"","history_revision":"invalid","sequence":0,"view":{},"request":{},"profile_identity":""}`,
		`{"history_revision":0}`,
	} {
		tx, err := d.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		raw := json.RawMessage(`[{"session_key":"invalid-inline","context_snapshot":` + snapshot + `}]`)
		err = RestoreChatControls(t.Context(), tx, map[string]json.RawMessage{"chat_control_sessions": raw})
		_ = tx.Rollback()
		if err == nil || !strings.Contains(err.Error(), "restore chat context snapshot:") {
			t.Fatalf("inline snapshot did not fail validation before insertion: %v", err)
		}
	}
}
