package sites

import "testing"

func TestBuildRoute_IncludesHTMLAndHTMIndexes(t *testing.T) {
	cfg := VhostConfig{
		ID:         "vhost-test",
		Hosts:      []string{"waterway.test"},
		RootPath:   "/tmp/waterway",
		PublicDir:  "public",
		PHPVersion: "8.3",
		HTTPS:      true,
		ServerRoot: "/tmp/server",
		EnableCORS: true,
	}

	route := buildRoute(cfg)

	// Navigate: handle[0] is the subroute; routes[0..1] = CORS, routes[2] = redirect, routes[3] = rewrite
	outerHandle := route["handle"].([]map[string]interface{})
	if len(outerHandle) == 0 {
		t.Fatal("expected outer handle")
	}
	subroutes := outerHandle[0]["routes"].([]map[string]interface{})
	if len(subroutes) < 4 {
		t.Fatalf("expected at least 4 subroutes, got %d", len(subroutes))
	}

	assertCORSSubroutes(t, subroutes[0], subroutes[1])

	// 2: canonical redirect block
	redirectMatch := subroutes[2]["match"].([]map[string]interface{})
	redirectFile := redirectMatch[0]["file"].(map[string]interface{})
	dirTry, ok := redirectFile["try_files"].([]string)
	if !ok {
		t.Fatalf("dir try_files not []string: %T", redirectFile["try_files"])
	}
	wantDir := []string{
		"{http.request.uri.path}/index.php",
		"{http.request.uri.path}/index.html",
		"{http.request.uri.path}/index.htm",
	}
	if len(dirTry) != len(wantDir) {
		t.Fatalf("dirTry len=%d, want %d: %v", len(dirTry), len(wantDir), dirTry)
	}
	for i := range wantDir {
		if dirTry[i] != wantDir[i] {
			t.Errorf("dirTry[%d]=%q want %q", i, dirTry[i], wantDir[i])
		}
	}

	// 3: rewrite block
	rewriteMatch := subroutes[3]["match"].([]map[string]interface{})
	rewriteFile := rewriteMatch[0]["file"].(map[string]interface{})
	rewriteTry, ok := rewriteFile["try_files"].([]string)
	if !ok {
		t.Fatalf("rewrite try_files not []string: %T", rewriteFile["try_files"])
	}
	wantRewrite := []string{
		"{http.request.uri.path}",
		"{http.request.uri.path}/index.php",
		"{http.request.uri.path}/index.html",
		"{http.request.uri.path}/index.htm",
		"index.html",
		"index.htm",
		"index.php",
	}
	if len(rewriteTry) != len(wantRewrite) {
		t.Fatalf("rewriteTry len=%d, want %d: %v", len(rewriteTry), len(wantRewrite), rewriteTry)
	}
	for i := range wantRewrite {
		if rewriteTry[i] != wantRewrite[i] {
			t.Errorf("rewriteTry[%d]=%q want %q", i, rewriteTry[i], wantRewrite[i])
		}
	}
}

func TestBuildHTTPSRedirectRoute_UsesProtocolMatcher(t *testing.T) {
	route := buildHTTPSRedirectRoute([]string{"myapp.test", "www.myapp.test"}, "vhost-myapp-test-https-redirect")

	if route["@id"] != "vhost-myapp-test-https-redirect" {
		t.Errorf("@id = %v", route["@id"])
	}

	matches := route["match"].([]map[string]interface{})
	if len(matches) != 1 {
		t.Fatalf("match len = %d, want 1", len(matches))
	}
	if _, hasExpr := matches[0]["expression"]; hasExpr {
		t.Error("redirect route must not use expression matcher (not in standard Caddy)")
	}
	if matches[0]["protocol"] != "http" {
		t.Errorf("protocol = %v, want http", matches[0]["protocol"])
	}
	hosts, ok := matches[0]["host"].([]string)
	if !ok || len(hosts) != 2 || hosts[0] != "myapp.test" {
		t.Errorf("host = %v", matches[0]["host"])
	}

	handle := route["handle"].([]map[string]interface{})
	if handle[0]["status_code"] != 308 {
		t.Errorf("status_code = %v, want 308", handle[0]["status_code"])
	}
}

func assertCORSSubroutes(t *testing.T, preflight, headers map[string]interface{}) {
	t.Helper()

	preflightMatch := preflight["match"].([]map[string]interface{})
	if preflightMatch[0]["method"].([]string)[0] != "OPTIONS" {
		t.Fatalf("preflight method = %v", preflightMatch[0]["method"])
	}
	if preflight["terminal"] != true {
		t.Fatal("preflight route must be terminal")
	}

	headersHandle := headers["handle"].([]map[string]interface{})
	if headersHandle[0]["handler"] != "headers" {
		t.Fatalf("headers handler = %v", headersHandle[0]["handler"])
	}
}

func TestBuildRoute_CORSDisabled_PHP(t *testing.T) {
	cfg := VhostConfig{
		ID:         "vhost-test",
		Hosts:      []string{"myapp.test"},
		RootPath:   "/tmp/myapp",
		PHPVersion: "8.3",
		ServerRoot: "/tmp/server",
		EnableCORS: false,
	}

	route := buildRoute(cfg)
	outerHandle := route["handle"].([]map[string]interface{})
	subroutes := outerHandle[0]["routes"].([]map[string]interface{})
	if len(subroutes) != 4 {
		t.Fatalf("expected 4 subroutes without CORS, got %d", len(subroutes))
	}
	if _, hasMethod := subroutes[0]["match"].([]map[string]interface{})[0]["method"]; hasMethod {
		t.Fatal("first subroute must not be OPTIONS preflight when CORS disabled")
	}
}

func TestBuildRoute_CORSDisabled_WS(t *testing.T) {
	cfg := VhostConfig{
		ID:         "vhost-ws",
		Hosts:      []string{"reverb.test"},
		SiteType:   "ws",
		WSUpstream: "127.0.0.1:8080",
		EnableCORS: false,
	}

	route := buildRoute(cfg)
	handle := route["handle"].([]map[string]interface{})
	if handle[0]["handler"] != "reverse_proxy" {
		t.Fatalf("ws route without CORS should use reverse_proxy directly, got %v", handle[0]["handler"])
	}
}

func TestBuildRoute_WSRoute(t *testing.T) {
	cfg := VhostConfig{
		ID:         "vhost-ws",
		Hosts:      []string{"reverb.test"},
		SiteType:   "ws",
		WSUpstream: "127.0.0.1:8080",
		EnableCORS: true,
	}
	route := buildRoute(cfg)
	if route["@id"] != "vhost-ws" {
		t.Error("ws route id wrong")
	}
	if _, hasHandle := route["handle"]; !hasHandle {
		t.Error("ws route missing handle")
	}

	outerHandle := route["handle"].([]map[string]interface{})
	subroutes := outerHandle[0]["routes"].([]map[string]interface{})
	if len(subroutes) < 3 {
		t.Fatalf("expected cors + proxy subroutes, got %d", len(subroutes))
	}
	assertCORSSubroutes(t, subroutes[0], subroutes[1])
	if subroutes[2]["handle"].([]map[string]interface{})[0]["handler"] != "reverse_proxy" {
		t.Error("ws route missing reverse_proxy handler")
	}
}
