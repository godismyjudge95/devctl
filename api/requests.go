package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	dbq "github.com/danielgormly/devctl/db/queries"
	"github.com/danielgormly/devctl/inspector"
	"github.com/danielgormly/devctl/php"
)

// inspectorRuleKeys are the settings that change the problem thresholds.
var inspectorRuleKeys = []string{"inspector_n_plus_one_min", "inspector_slow_query_ms", "inspector_slow_request_ms"}

// prependKeys are the settings written into prepend.php and inspector.php.
var prependKeys = []string{"dump_to_page", "inspector_enabled"}

// settingChecks validate a PUT /api/settings value and return it normalized.
var settingChecks = map[string]func(string) (string, error){
	"dump_to_page":              checkBool,
	"inspector_enabled":         checkBool,
	"inspector_n_plus_one_min":  checkIntMin(2),
	"inspector_slow_query_ms":   checkNonNegative,
	"inspector_slow_request_ms": checkNonNegative,
	"editor_url":                checkEditorURL,
}

func checkBool(v string) (string, error) {
	b, err := strconv.ParseBool(v)
	if err != nil {
		return "", fmt.Errorf("must be true or false")
	}
	return strconv.FormatBool(b), nil
}

func checkIntMin(min int) func(string) (string, error) {
	return func(v string) (string, error) {
		n, err := strconv.Atoi(v)
		if err != nil || n < min {
			return "", fmt.Errorf("must be a whole number of at least %d", min)
		}
		return strconv.Itoa(n), nil
	}
}

func checkNonNegative(v string) (string, error) {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return "", fmt.Errorf("must be a number of milliseconds, 0 or more (0 turns the rule off)")
	}
	return strconv.FormatFloat(f, 'f', -1, 64), nil
}

// checkEditorURL keeps the dashboard's file links on an editor scheme: the
// value becomes an href on every caller link.
func checkEditorURL(v string) (string, error) {
	scheme, _, ok := strings.Cut(v, ":")
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "javascript", "data", "vbscript":
		ok = false
	}
	if !ok || !strings.Contains(v, "{file}") {
		return "", fmt.Errorf("must be an editor URL with {file}, for example vscode://file/{file}:{line}")
	}
	return v, nil
}

// SettingsLookup returns a getter for a setting: the value in input when
// present, else the stored value, else the default.
func SettingsLookup(ctx context.Context, q *dbq.Queries, input map[string]string) func(string) string {
	return func(key string) string {
		if v := input[key]; v != "" {
			return v
		}
		if v, err := q.GetSetting(ctx, key); err == nil && v != "" {
			return v
		}
		return settingDefaults[key]
	}
}

// PrependConfig builds the PHP prepend settings. dumpAddr is the address the
// dump listener uses.
func PrependConfig(get func(string) string, dumpAddr string) php.PrependConfig {
	toPage, _ := strconv.ParseBool(get("dump_to_page"))
	enabled, _ := strconv.ParseBool(get("inspector_enabled"))
	return php.PrependConfig{ToPage: toPage, DumpAddr: dumpAddr, Inspector: enabled}
}

// InspectorRules builds the problem thresholds from the settings.
func InspectorRules(get func(string) string) inspector.Rules {
	n, _ := strconv.Atoi(get("inspector_n_plus_one_min"))
	slowQuery, _ := strconv.ParseFloat(get("inspector_slow_query_ms"), 64)
	slowRequest, _ := strconv.ParseFloat(get("inspector_slow_request_ms"), 64)
	return inspector.Rules{NPlusOneMin: n, SlowQueryMs: slowQuery, SlowRequestMs: slowRequest}
}

func (s *Server) handleListRequests(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	problems, _ := strconv.ParseBool(q.Get("problems"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	writeJSON(w, s.dumps.Requests.List(inspector.Filter{
		Site:     q.Get("site"),
		Kind:     q.Get("kind"),
		Problems: problems,
		Limit:    limit,
	}))
}

func (s *Server) handleGetRequest(w http.ResponseWriter, r *http.Request) {
	d, ok := s.dumps.Requests.Get(r.PathValue("id"))
	if !ok {
		writeError(w, "request not found; the inspector keeps only the newest requests", http.StatusNotFound)
		return
	}
	writeJSON(w, d)
}

func (s *Server) handleClearRequests(w http.ResponseWriter, r *http.Request) {
	s.dumps.Requests.Clear()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRequestsWS(w http.ResponseWriter, r *http.Request) {
	serveHub(w, r, s.dumps.RequestsHub)
}
