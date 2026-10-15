package server

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/artex/internal/dbtest"
	"github.com/Autumn-27/artex/locale"
)

func TestOperationsMCPValidationLanguages(t *testing.T) {
	for _, transport := range []string{"stdio", "http", "sse", "custom%raw"} {
		_, err := connectMCP(context.Background(), &db.MCPServer{Transport: transport})
		if err == nil {
			t.Fatalf("%s accepted without configuration", transport)
		}
		en := locale.ErrorMessage(locale.En, err)
		ko := locale.ErrorMessage(locale.Ko, err)
		if en == ko {
			t.Fatalf("untranslated %s: %q", transport, en)
		}
		if transport == "custom%raw" && !strings.Contains(ko, transport) {
			t.Fatalf("raw transport changed: %q", ko)
		}
	}
}

func TestOperationsCustomToolLocalePreservesSchema(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","description":"User description 원본","properties":{"args":{"type":"string","description":"User args 100%"}}}`)
	got := ensureSchemaForLanguage(raw, locale.Ko)
	if got["description"] != "User description 원본" {
		t.Fatal("user schema translated")
	}
	props := got["properties"].(map[string]any)
	if props["args"].(map[string]any)["description"] != "User args 100%" {
		t.Fatal("user parameter description changed")
	}
	fallback := ensureSchemaForLanguage(nil, locale.Ko)["properties"].(map[string]any)["args"].(map[string]any)
	if fallback["description"] != "명령/인수(자유 형식 텍스트)" {
		t.Fatalf("fallback=%v", fallback)
	}
	s := &Server{}
	ctx := locale.WithLang(context.Background(), locale.Ko)
	result, err := s.runCommandTool(ctx, json.RawMessage(`{}`), nil, nil)
	if err != nil || !result.IsError || !strings.Contains(result.Flatten(), "command가 비어 있습니다") {
		t.Fatalf("command result=%+v err=%v", result, err)
	}
	result, err = s.runHTTPTool(ctx, json.RawMessage(`{}`), nil, nil)
	if err != nil || !result.IsError || !strings.Contains(result.Flatten(), "HTTP URL이 비어 있습니다") {
		t.Fatalf("HTTP result=%+v err=%v", result, err)
	}
}

func TestOperationsAttachmentAndWorkspaceDataRemainVerbatim(t *testing.T) {
	dir := t.TempDir()
	msg := "User text 100% 원본"
	name := "attachment.txt"
	got := composeAgentMessageForLanguage(msg, []chatAttachment{{Path: name, Size: 12}}, dir, locale.Ko)
	if !strings.HasPrefix(got, msg) || !strings.Contains(got, "사용자가 업로드한 첨부 파일") || !strings.Contains(got, filepath.Join(dir, name)) {
		t.Fatalf("attachment message=%q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(msg), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{m: &Manager{dir: dir}}
	rec := httptest.NewRecorder()
	s.wsRead(&localeWriter{rec, locale.Ko}, httptest.NewRequest(http.MethodGet, "/api/workspace/read?path="+name, nil))
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || payload.Content != msg {
		t.Fatalf("workspace content changed: %d %+v", rec.Code, payload)
	}
	rec = httptest.NewRecorder()
	s.wsRead(&localeWriter{rec, locale.Ko}, httptest.NewRequest(http.MethodGet, "/api/workspace/read?path=missing", nil))
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "파일을 찾을 수 없습니다") {
		t.Fatalf("workspace error=%d %s", rec.Code, rec.Body.String())
	}
}

func TestOperationsSyncParseErrorsUseRequestLanguage(t *testing.T) {
	for _, kind := range []string{"subdomain", "app", "service"} {
		got := (&Server{}).ssIngestForLanguage(nil, kind, json.RawMessage(`{`), map[string]int{}, locale.Ko)
		if !strings.Contains(got, "해석 실패") || !strings.Contains(got, "unexpected end of JSON input") {
			t.Fatalf("%s lost localized prefix/raw decoder error: %q", kind, got)
		}
	}
}

func TestOperationsCustomToolValidationHTTP(t *testing.T) {
	dsn := dbtest.DSN(t)
	pg, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	s := &Server{m: &Manager{pg: pg}}
	for _, lang := range []locale.Lang{locale.En, locale.Ko} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/tools/custom", strings.NewReader(`{"key":"INVALID","kind":"command"}`))
		s.pgCreateCustomTool(&localeWriter{rec, lang}, req)
		want := locale.Text(lang, "key must start with a lowercase letter and contain only lowercase letters, digits, or underscores")
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("%s response=%d %s", lang, rec.Code, rec.Body.String())
		}
	}
}

func TestOperationsMessageCatalogCoverage(t *testing.T) {
	count := 0
	for _, name := range []string{"customtool", "sync_scopesentry", "workspace", "chatupload", "commands", "llmpool", "llmretry", "mcpdiscover"} {
		f, err := parser.ParseFile(token.NewFileSet(), name+".go", nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			index := -1
			switch fn := c.Fun.(type) {
			case *ast.Ident:
				if fn.Name == "writeErr" {
					index = 2
				}
			case *ast.SelectorExpr:
				if pkg, ok := fn.X.(*ast.Ident); ok && pkg.Name == "locale" {
					switch fn.Sel.Name {
					case "Text":
						index = 1
					case "Errorf", "NewError":
						index = 0
					}
				}
			}
			if index < 0 || len(c.Args) <= index {
				return true
			}
			lit, ok := c.Args[index].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			key, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatal(err)
			}
			en, found := locale.Lookup(locale.En, key)
			ko, kfound := locale.Lookup(locale.Ko, key)
			if !found || !kfound || en == ko {
				t.Errorf("%s: missing Korean key %q", name, key)
			}
			count++
			return true
		})
	}
	if count < 50 {
		t.Fatalf("only %d message calls checked", count)
	}
}
