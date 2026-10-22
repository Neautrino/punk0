package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// settingsReq posts body (JSON, or a query when body is nil) the way the
// browser does: same-origin on 127.0.0.1.
func settingsReq(t *testing.T, h http.Handler, url string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(http.MethodPost, url, rd)
	req.Host = "127.0.0.1:7777"
	req.Header.Set("Origin", "http://127.0.0.1:7777")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSettingsWorkbenchRoute(t *testing.T) {
	isolateSettings(t)
	newTestStore(t)
	wb := testWorkbench(t, "/", t.TempDir(), "", nil)

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req.Host = "127.0.0.1:7777"
	rec := httptest.NewRecorder()
	wb.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"schema"`) {
		t.Fatalf("GET: %d %s", rec.Code, rec.Body.String())
	}

	cross := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"tips.enabled":false}`))
	cross.Host = "127.0.0.1:7777"
	cross.Header.Set("Origin", "http://evil.example")
	rec = httptest.NewRecorder()
	wb.ServeHTTP(rec, cross)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST: %d", rec.Code)
	}

	if rec := settingsReq(t, wb, "/api/settings", map[string]any{"tips.enabled": false}); rec.Code != 200 {
		t.Fatalf("POST: %d %s", rec.Code, rec.Body.String())
	}
	if readMergedSettingsMap()["tips.enabled"] != false {
		t.Fatal("tips.enabled not saved")
	}
}

func TestSettingsExecGateOffLoopback(t *testing.T) {
	isolateSettings(t)
	srv := agentServer(t, t.TempDir(), "echo")
	srv.localOnly = false
	if err := updateSettingsMap(map[string]any{"terminal.shell": "/bin/bash"}); err != nil {
		t.Fatal(err)
	}

	refused := []struct {
		name, url string
		body      any
	}{
		{"key", "/api/settings", map[string]any{"terminal.shell": "/bin/evil"}},
		{"args", "/api/settings", map[string]any{"agents.claude.args": "--x"}},
		{"query", "/api/settings?terminal.env=X%3D1", nil},
		{"raw", "/api/settings", map[string]any{"raw": `{"terminal.shell":"/bin/evil"}`}},
		{"harness", "/api/settings", map[string]any{"agent.harness": "sh -c {prompt}"}},
		{"agent spelling", "/api/settings", map[string]any{"agent": "sh -c {prompt}"}},
		{"select", "/api/agent/select?name=" + "sh%20-c%20%7Bprompt%7D", nil},
	}
	for _, c := range refused {
		rec := settingsReq(t, srv, c.url, c.body)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "can only be changed when punk0 listens on 127.0.0.1") {
			t.Errorf("%s: want 403, got %d %s", c.name, rec.Code, rec.Body.String())
		}
	}
	if got := readMergedSettingsMap()["terminal.shell"]; got != "/bin/bash" {
		t.Fatalf("terminal.shell changed to %v", got)
	}

	// The modal's Save sends the whole merged map: unchanged gated keys pass.
	full := readMergedSettingsMap()
	full["tips.enabled"] = false
	if rec := settingsReq(t, srv, "/api/settings", full); rec.Code != 200 {
		t.Fatalf("full save: %d %s", rec.Code, rec.Body.String())
	}
	if rec := settingsReq(t, srv, "/api/settings", map[string]any{"terminal.shell": nil}); rec.Code != 200 {
		t.Fatalf("removal: %d %s", rec.Code, rec.Body.String())
	}
	if _, ok := readSettingsRawMap()["terminal.shell"]; ok {
		t.Fatal("terminal.shell not removed")
	}
	if rec := settingsReq(t, srv, "/api/agent/select?name=claude", nil); rec.Code == http.StatusForbidden {
		t.Fatalf("preset select refused: %s", rec.Body.String())
	}
}

func TestSettingsExecGateLoopback(t *testing.T) {
	isolateSettings(t)
	srv := agentServer(t, t.TempDir(), "echo")
	srv.localOnly = true
	for _, body := range []map[string]any{
		{"terminal.shell": "/bin/zsh"},
		{"raw": `{"agents.codex.args":"--yolo"}`},
		{"agent.harness": "echo {prompt}"},
	} {
		if rec := settingsReq(t, srv, "/api/settings", body); rec.Code != 200 {
			t.Errorf("%v: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	if rec := settingsReq(t, srv, "/api/settings?terminal.fontSize=14", nil); rec.Code != 200 {
		t.Errorf("query: %d %s", rec.Code, rec.Body.String())
	}
	if got := readMergedSettingsMap()["terminal.fontSize"]; got != 14.0 {
		t.Errorf("query fontSize = %v", got)
	}
}

func TestSettingsValidation(t *testing.T) {
	isolateSettings(t)
	srv := agentServer(t, t.TempDir(), "echo")
	srv.localOnly = true
	for _, c := range []struct {
		body any
		want string
	}{
		{map[string]any{"tips.enabled": "yes"}, "tips.enabled"},
		{map[string]any{"workbench.colorTheme": "neon"}, "workbench.colorTheme"},
		{map[string]any{"agents.hidden": []string{"claude", "vim"}}, "agents.hidden"},
		{map[string]any{"terminal.fontSize": 99}, "terminal.fontSize"},
		{map[string]any{"terminal.env": "A=1\nnope"}, "line 2"},
		{map[string]any{"raw": `{"terminal.fontSize":"big"}`}, "terminal.fontSize"},
	} {
		rec := settingsReq(t, srv, "/api/settings", c.body)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%v: want 400 naming %s, got %d %s", c.body, c.want, rec.Code, rec.Body.String())
		}
	}
	if rec := settingsReq(t, srv, "/api/settings", map[string]any{"agents.hidden": []string{"shell"}, "my.custom": 3}); rec.Code != 200 {
		t.Fatalf("valid save: %d %s", rec.Code, rec.Body.String())
	}
}
