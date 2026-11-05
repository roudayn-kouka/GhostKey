package dashboard

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, store Store, demo bool) *Server {
	t.Helper()
	srv, err := NewServer(store, Options{Demo: demo, AllowedHosts: DefaultAllowedHosts(namespace, "ghostkey-dashboard")})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func do(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "localhost:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// validateBodies are Secret manifests posted to /api/validate: they carry
// real values, which must never come back.
var validateBodies = []string{
	// Valid, plain-text value.
	`apiVersion: v1
kind: Secret
metadata:
  name: stripe
  labels: {ghostkey.io/enabled: "true"}
  annotations: {ghostkey.io/hosts: api.stripe.com}
stringData:
  api-key: ` + realStripe + "\n",
	// Valid, base64 value.
	`apiVersion: v1
kind: Secret
metadata:
  name: stripe
  labels: {ghostkey.io/enabled: "true"}
data:
  api-key: ` + base64.StdEncoding.EncodeToString([]byte(realWebhook)) + "\n",
	// Rejected (too long), value in the error path.
	`kind: Secret
metadata: {name: s, labels: {ghostkey.io/enabled: "true"}}
stringData: {k: ` + strings.Repeat(realOrphan, 5) + "}\n",
	// Invalid base64: the decoder error must not quote the input.
	`kind: Secret
metadata: {name: s, labels: {ghostkey.io/enabled: "true"}}
data: {k: "` + realPlain + `!!"}` + "\n",
	// Broken YAML around a value.
	"kind: Secret\nstringData:\n  k: " + realPlain + "\n    : : [\n",
	// Not a Secret.
	"kind: ConfigMap\ndata: {k: " + realPlain + "}\n",
}

// TestAPINeverLeaksSecretData calls every API route, on the Kubernetes store
// and on the demo store, and checks that no response contains a real secret
// value, its base64 form, a full placeholder or a placeholder's random tail.
func TestAPINeverLeaksSecretData(t *testing.T) {
	var forbidden []string
	for _, v := range fixtureValues {
		forbidden = append(forbidden, v, base64.StdEncoding.EncodeToString([]byte(v)))
		if tail, ok := strings.CutPrefix(v, "kl::"); ok {
			forbidden = append(forbidden, tail)
		}
	}
	for _, v := range fixtureValues {
		// Also any distinctive 12-byte window, e.g. a truncated value.
		for i := 0; i+12 <= len(v); i += 4 {
			if w := v[i : i+12]; !strings.HasPrefix(w, "REAL-SENTINE") && !strings.HasPrefix(w, "kl::SHADOWTA") {
				forbidden = append(forbidden, w)
			}
		}
	}

	stores := map[string]Store{"kube": newFixtureStore(t), "demo": NewDemoStore()}
	for name, store := range stores {
		t.Run(name, func(t *testing.T) {
			srv := newTestServer(t, store, name == "demo")
			var bodies []string
			for _, path := range []string{"/api/overview", "/api/secrets", "/api/pods", "/api/violations", "/api/unknown"} {
				rec := do(t, srv, http.MethodGet, path, "")
				if path != "/api/unknown" && rec.Code != http.StatusOK {
					t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body)
				}
				bodies = append(bodies, rec.Body.String())
			}
			for _, b := range validateBodies {
				bodies = append(bodies, do(t, srv, http.MethodPost, "/api/validate", b).Body.String())
			}
			for _, body := range bodies {
				for _, f := range forbidden {
					if strings.Contains(body, f) {
						t.Errorf("response leaks %q:\n%s", f, body)
					}
				}
			}
		})
	}
}

func decodeEnvelope[T any](t *testing.T, rec *httptest.ResponseRecorder) (Envelope, T) {
	t.Helper()
	var raw struct {
		Envelope
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode %s: %v", rec.Body, err)
	}
	var data T
	if err := json.Unmarshal(raw.Data, &data); err != nil {
		t.Fatalf("decode data %s: %v", raw.Data, err)
	}
	return raw.Envelope, data
}

func TestValidateEndpoint(t *testing.T) {
	srv := newTestServer(t, NewDemoStore(), false)
	for i, tc := range []struct {
		body        string
		wantAllowed bool
		wantEnabled bool
		wantMsg     string
	}{
		{validateBodies[0], true, true, "valid"},
		{validateBodies[1], true, true, "valid"},
		{validateBodies[2], false, true, "maximum 128 allowed"},
		{"apiVersion: v1\nkind: Secret\nmetadata: {name: plain}\nstringData: {k: short}\n", true, false, "not ghostkey-enabled"},
		{"kind: Secret\nmetadata:\n  name: s\n  labels: {ghostkey.io/enabled: \"true\"}\n  annotations: {ghostkey.io/port: \"99999\"}\nstringData: {k: long-enough-value}\n",
			false, true, "invalid ghostkey.io/port annotation"},
	} {
		rec := do(t, srv, http.MethodPost, "/api/validate", tc.body)
		if rec.Code != http.StatusOK {
			t.Fatalf("case %d: status %d: %s", i, rec.Code, rec.Body)
		}
		_, got := decodeEnvelope[ValidateResult](t, rec)
		if got.Allowed != tc.wantAllowed || got.Enabled != tc.wantEnabled || !strings.Contains(got.Message, tc.wantMsg) {
			t.Errorf("case %d: got %+v, want allowed=%v enabled=%v message containing %q", i, got, tc.wantAllowed, tc.wantEnabled, tc.wantMsg)
		}
	}
}

func TestValidateEndpointErrors(t *testing.T) {
	srv := newTestServer(t, NewDemoStore(), false)
	for name, tc := range map[string]struct {
		body     string
		wantCode int
		wantErr  string
	}{
		"empty":       {"  \n# only a comment\n", http.StatusBadRequest, "paste a Secret"},
		"several":     {"kind: Secret\n---\nkind: Secret\n", http.StatusBadRequest, "single Secret"},
		"leading ---": {"---\nkind: Secret\nmetadata: {name: s}\n", http.StatusOK, ""},
		"bad base64":  {validateBodies[3], http.StatusBadRequest, "base64"},
		"bad yaml":    {validateBodies[4], http.StatusBadRequest, "invalid Secret YAML (line"},
		"not secret":  {validateBodies[5], http.StatusBadRequest, `expected kind Secret, got \"ConfigMap\"`},
		"too large":   {strings.Repeat("a", maxValidateBody+1), http.StatusRequestEntityTooLarge, "at most 64 KiB"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := do(t, srv, http.MethodPost, "/api/validate", tc.body)
			if rec.Code != tc.wantCode || !strings.Contains(rec.Body.String(), tc.wantErr) {
				t.Errorf("got %d %s, want %d containing %q", rec.Code, rec.Body, tc.wantCode, tc.wantErr)
			}
		})
	}
}

func TestDemoFlag(t *testing.T) {
	for _, demo := range []bool{false, true} {
		srv := newTestServer(t, NewDemoStore(), demo)
		env, _ := decodeEnvelope[Overview](t, do(t, srv, http.MethodGet, "/api/overview", ""))
		if env.Demo != demo {
			t.Errorf("demo = %v, want %v", env.Demo, demo)
		}
	}
}

func TestHostCheck(t *testing.T) {
	srv := newTestServer(t, NewDemoStore(), false)
	for host, want := range map[string]int{
		"localhost:8090":     http.StatusOK,
		"127.0.0.1:8090":     http.StatusOK,
		"[::1]:8090":         http.StatusOK,
		"LOCALHOST":          http.StatusOK,
		"ghostkey-dashboard": http.StatusOK,
		"ghostkey-dashboard.ghostkey-system.svc:8090": http.StatusOK,
		"evil.example:8090":                           http.StatusMisdirectedRequest,
		"":                                            http.StatusMisdirectedRequest,
	} {
		req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Host %q: status %d, want %d", host, rec.Code, want)
		}
	}

	any, err := NewServer(NewDemoStore(), Options{AllowedHosts: []string{"*"}})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	req.Host = "whatever.example"
	rec := httptest.NewRecorder()
	any.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("wildcard host: status %d", rec.Code)
	}

	if _, err := NewServer(NewDemoStore(), Options{AllowedHosts: []string{" "}}); err == nil {
		t.Error("NewServer accepted an empty allowed-host list")
	}
}

func TestMethodsAndStatic(t *testing.T) {
	srv := newTestServer(t, NewDemoStore(), false)
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/api/overview", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/api/secrets", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/validate", http.StatusMethodNotAllowed},
		{http.MethodPut, "/", http.StatusMethodNotAllowed},
		{http.MethodGet, "/api/nope", http.StatusNotFound},
		{http.MethodGet, "/", http.StatusOK},
		{http.MethodGet, "/app.js", http.StatusOK},
		{http.MethodGet, "/app.css", http.StatusOK},
		{http.MethodGet, "/logo.svg", http.StatusOK},
		{http.MethodGet, "/missing.js", http.StatusNotFound},
	} {
		if rec := do(t, srv, tc.method, tc.path, ""); rec.Code != tc.want {
			t.Errorf("%s %s = %d, want %d", tc.method, tc.path, rec.Code, tc.want)
		}
	}
	rec := do(t, srv, http.MethodGet, "/", "")
	body, _ := io.ReadAll(rec.Body)
	if !strings.Contains(string(body), "GhostKey") || !strings.Contains(string(body), "app.js") {
		t.Errorf("index.html not served: %s", body)
	}
}

func TestSecurityHeaders(t *testing.T) {
	srv := newTestServer(t, NewDemoStore(), false)
	for _, path := range []string{"/", "/api/overview"} {
		h := do(t, srv, http.MethodGet, path, "").Header()
		for name, want := range map[string]string{
			"X-Content-Type-Options": "nosniff",
			"X-Frame-Options":        "DENY",
			"Cache-Control":          "no-store",
			"Referrer-Policy":        "no-referrer",
		} {
			if got := h.Get(name); got != want {
				t.Errorf("%s: %s = %q, want %q", path, name, got, want)
			}
		}
		if csp := h.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "unsafe-inline") {
			t.Errorf("%s: weak CSP %q", path, csp)
		}
	}
	if ct := do(t, srv, http.MethodGet, "/api/pods", "").Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("API Content-Type = %q", ct)
	}
}
