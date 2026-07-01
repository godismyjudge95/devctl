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
	}

	route := buildRoute(cfg)

	// Navigate: handle[0] is the subroute; routes[0] = redirect, routes[1] = rewrite
	outerHandle := route["handle"].([]map[string]interface{})
	if len(outerHandle) == 0 {
		t.Fatal("expected outer handle")
	}
	subroutes := outerHandle[0]["routes"].([]map[string]interface{})
	if len(subroutes) < 2 {
		t.Fatalf("expected at least 2 subroutes, got %d", len(subroutes))
	}

	// 0: canonical redirect block
	redirectMatch := subroutes[0]["match"].([]map[string]interface{})
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

	// 1: rewrite block
	rewriteMatch := subroutes[1]["match"].([]map[string]interface{})
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

func TestBuildRoute_WSRoute(t *testing.T) {
	cfg := VhostConfig{
		ID:         "vhost-ws",
		Hosts:      []string{"reverb.test"},
		SiteType:   "ws",
		WSUpstream: "127.0.0.1:8080",
	}
	route := buildRoute(cfg)
	if route["@id"] != "vhost-ws" {
		t.Error("ws route id wrong")
	}
	// should not have the php subroute structure
	if _, hasHandle := route["handle"]; !hasHandle {
		t.Error("ws route missing handle")
	}
}
