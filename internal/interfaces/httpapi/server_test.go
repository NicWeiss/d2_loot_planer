package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"loot-planner/internal/application"
	"loot-planner/internal/infrastructure/storage"
	"loot-planner/internal/interfaces/httpapi"
	tk "loot-planner/internal/testkit"
)

const R = "/api/realms/softcore%20ladder"

type env struct {
	t    *testing.T
	srv  *httptest.Server
	c    *http.Client
	site *tk.FakeSite
}

func setup(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	site := &tk.FakeSite{}
	market := application.NewMarketService(site, storage.NewSnapshots(dir), []string{"softcore ladder", "hardcore ladder"}, 0)
	stash := application.NewStashService(site, storage.NewSessions(dir), market, 24*time.Hour)
	front := fstest.MapFS{"index.html": {Data: []byte("<title>Карта обмена</title>")}}
	access := application.NewAccess([]string{"nicweiss"})
	srv := httptest.NewServer((&httpapi.Server{Market: market, Stash: stash, Queries: &application.Queries{Market: market, Access: access},
		Access: access, Frontend: front}).Handler())
	t.Cleanup(srv.Close)
	// an empty data dir makes the app fetch the market on start
	market.Schedule(0)
	jar, _ := cookiejar.New(nil)
	e := &env{t, srv, &http.Client{Jar: jar}, site}
	for i := 0; i < 200 && market.State() == nil; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	return e
}

func (e *env) do(method, path string, body any, contentType string) (int, map[string]any) {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rd)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := e.c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (e *env) get(path string) (int, map[string]any) { return e.do("GET", path, nil, "") }
func (e *env) post(path string, body any) (int, map[string]any) {
	if body == nil {
		body = map[string]any{}
	}
	return e.do("POST", path, body, "application/json")
}

func TestFirstStartFetchesMarket(t *testing.T) {
	e := setup(t)
	if e.site.Fetches != 1 {
		t.Errorf("fetches %d", e.site.Fetches)
	}
	_, meta := e.get("/api/meta")
	if meta["ready"] != true || meta["defaultRealm"] != "softcore ladder" || meta["names"].(map[string]any)["r25"] == nil {
		t.Errorf("meta %v", meta)
	}
}

func TestMarketQueries(t *testing.T) {
	e := setup(t)
	if _, d := e.get(R + "/target?code=r25"); d["best"].(map[string]any)["url"] == nil {
		t.Error("target has a best lot")
	}
	if _, d := e.get(R + "/offers?code=r25"); len(d["offers"].([]any)) == 0 {
		t.Error("offers")
	}
	_, rates := e.get(R + "/rates")
	for _, r := range rates["rows"].([]any) {
		if row := r.(map[string]any); row["code"] == "r25" && row["askVsFair"].(float64) >= 0 {
			t.Error("the live Gul is below fair value")
		}
	}
	if _, d := e.get(R + "/map"); len(d["nodes"].([]any)) == 0 {
		t.Error("map nodes")
	}
	if code, _ := e.get("/api/realms/nowhere/rates"); code != 404 {
		t.Errorf("unknown realm: %d", code)
	}
}

func TestStateChangesNeedJSON(t *testing.T) {
	e := setup(t)
	if code, _ := e.do("POST", "/api/market/refresh", "x", "text/plain"); code != 415 {
		t.Errorf("status %d", code)
	}
}

func TestLoginWithoutRememberSignsOut(t *testing.T) {
	e := setup(t)
	code, d := e.post("/api/stash/login", map[string]any{"login": "nic", "password": "secret"})
	if code != 200 || d["remembered"] != false {
		t.Fatalf("%d %v", code, d)
	}
	if e.site.LogoutCount() != 1 {
		t.Errorf("logouts %d", e.site.LogoutCount())
	}
	_, st := e.get(R + "/stash")
	if st["owner"] != "nic" || st["appraisal"].(map[string]any)["totals"].(map[string]any)["currencyPositions"].(float64) != 5 {
		t.Errorf("stash %v", st)
	}
	if code, _ := e.post("/api/stash/refresh", nil); code != 401 {
		t.Errorf("no remembered login: refresh = %d", code)
	}
}

func TestWrongPassword(t *testing.T) {
	e := setup(t)
	code, d := e.post("/api/stash/login", map[string]any{"login": "nic", "password": "nope"})
	if code != 401 || !strings.Contains(d["error"].(string), "Invalid") {
		t.Errorf("%d %v", code, d)
	}
}

func TestRememberedLoginRefreshesUntilExpired(t *testing.T) {
	e := setup(t)
	if _, d := e.post("/api/stash/login", map[string]any{"login": "nic", "password": "secret", "remember": true}); d["remembered"] != true {
		t.Fatalf("%v", d)
	}
	if e.site.LogoutCount() != 0 {
		t.Error("a remembered login stays signed in")
	}
	if code, _ := e.post("/api/stash/refresh", nil); code != 200 {
		t.Errorf("refresh %d", code)
	}
	e.site.Expired = true
	code, d := e.post("/api/stash/refresh", nil)
	if code != 401 || !strings.Contains(d["error"].(string), "войди заново") {
		t.Errorf("%d %v", code, d)
	}
	if _, st := e.get(R + "/stash"); st["loaded"] != true {
		t.Error("the last stash stays")
	}
}

func TestRouteUsesTheSessionsStash(t *testing.T) {
	e := setup(t)
	_, before := e.post(R+"/route", map[string]any{"target": "r25"})
	if before["stash"].(map[string]any)["loaded"] != false {
		t.Error("no stash yet")
	}
	e.post("/api/stash/upload", map[string]any{"name": "export", "data": map[string]any{"data": json.RawMessage(tk.StashExport())}})
	_, after := e.post(R+"/route", map[string]any{"target": "r25"})
	st := after["stash"].(map[string]any)
	if st["loaded"] != true || st["owner"] != "export" || st["realm"] != true || after["route"].(map[string]any)["ok"] != true {
		t.Errorf("%v", after)
	}
}

func TestResetForgetsEverything(t *testing.T) {
	e := setup(t)
	e.post("/api/stash/login", map[string]any{"login": "nic", "password": "secret", "remember": true})
	if code, _ := e.do("DELETE", "/api/stash", nil, "application/json"); code != 200 || e.site.LogoutCount() != 1 {
		t.Errorf("reset %d, logouts %d", code, e.site.LogoutCount())
	}
	if _, st := e.get(R + "/stash"); st["loaded"] != false {
		t.Errorf("%v", st)
	}
}

func TestBadUploadAndValidation(t *testing.T) {
	e := setup(t)
	if code, _ := e.post("/api/stash/upload", map[string]any{"data": map[string]any{"x": 1}}); code != 400 {
		t.Errorf("upload %d", code)
	}
	if code, _ := e.post(R+"/route", map[string]any{"target": "r25", "qty": 99}); code != 422 {
		t.Errorf("qty %d", code)
	}
}

func TestFrontEndIsServed(t *testing.T) {
	e := setup(t)
	resp, err := e.c.Get(e.srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "Карта обмена") {
		t.Errorf("%d %s", resp.StatusCode, b)
	}
}

func TestStrategy(t *testing.T) {
	e := setup(t)
	code, d := e.post(R+"/strategy", map[string]any{"target": "r25"})
	if code != 200 {
		t.Fatalf("%d %v", code, d)
	}
	list := d["strategies"].([]any)
	if len(list) == 0 || list[0].(map[string]any)["ok"] != true {
		t.Fatalf("keys buy Mal, Mal buys the cheap Gul: %v", d)
	}
	farm := list[0].(map[string]any)["farm"].([]any)[0].(map[string]any)
	if farm["code"] != "pk2" || farm["need"].(float64) <= 0 {
		t.Errorf("farm %v", farm)
	}
	if wtb := d["wtb"].([]any); len(wtb) == 0 || wtb[0].(map[string]any)["code"] != "pk2" {
		t.Errorf("a fair-price WTB paid in keys: %v", wtb)
	}
}

func TestOnlyAdminRefreshesMarket(t *testing.T) {
	e := setup(t)
	if code, d := e.post("/api/market/refresh", nil); code != 403 {
		t.Errorf("anonymous: %d %v", code, d)
	}
	// a file named after the admin proves nothing
	e.post("/api/stash/upload", map[string]any{"name": "nicweiss", "data": map[string]any{"data": json.RawMessage(tk.StashExport())}})
	if code, _ := e.post("/api/market/refresh", nil); code != 403 {
		t.Errorf("uploaded file: %d", code)
	}
	e.post("/api/stash/login", map[string]any{"login": "someone", "password": "secret"})
	if code, _ := e.post("/api/market/refresh", nil); code != 403 {
		t.Errorf("other login: %d", code)
	}
	if _, meta := e.get("/api/meta"); meta["admin"] != false {
		t.Errorf("meta admin for someone: %v", meta["admin"])
	}
	e.post("/api/stash/login", map[string]any{"login": "NicWeiss", "password": "secret"})
	if _, meta := e.get("/api/meta"); meta["admin"] != true {
		t.Errorf("meta admin for nicweiss: %v", meta["admin"])
	}
	if code, d := e.post("/api/market/refresh", nil); code != 202 {
		t.Errorf("admin: %d %v", code, d)
	}
	e.do("DELETE", "/api/stash", nil, "application/json")
	if code, _ := e.post("/api/market/refresh", nil); code != 403 {
		t.Errorf("after reset: %d", code)
	}
}
