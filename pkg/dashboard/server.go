package dashboard

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"github.com/roudayna/ghostkey/pkg/webhook"
)

//go:embed web
var webFS embed.FS

// maxValidateBody bounds the Secret YAML /api/validate accepts.
const maxValidateBody = 64 << 10

// requestTimeout bounds how long one API request may read the cluster.
const requestTimeout = 20 * time.Second

// Options configure the dashboard server.
type Options struct {
	// Demo marks every response as demonstration data.
	Demo bool
	// AllowedHosts are the Host header values (without port) the server
	// answers, against DNS rebinding: a page on another site that rebinds
	// its name to 127.0.0.1 must not be able to read the API through a
	// `kubectl port-forward`. "*" allows any host.
	AllowedHosts []string
	Log          *zap.SugaredLogger
}

// DefaultAllowedHosts are the Host values reachable through `kubectl
// port-forward` and, in namespace, through the dashboard Service.
func DefaultAllowedHosts(namespace, service string) []string {
	hosts := []string{"localhost", "127.0.0.1", "::1"}
	if service != "" {
		hosts = append(hosts, service)
		if namespace != "" {
			hosts = append(hosts,
				service+"."+namespace,
				service+"."+namespace+".svc",
				service+"."+namespace+".svc.cluster.local")
		}
	}
	return hosts
}

// Server is the dashboard's HTTP handler.
type Server struct {
	store   Store
	opts    Options
	allowed map[string]bool
	anyHost bool
	now     func() time.Time
	mux     *http.ServeMux
}

// NewServer returns the dashboard handler over store.
func NewServer(store Store, opts Options) (*Server, error) {
	if opts.Log == nil {
		opts.Log = zap.NewNop().Sugar()
	}
	s := &Server{store: store, opts: opts, allowed: map[string]bool{}, now: time.Now, mux: http.NewServeMux()}
	for _, h := range opts.AllowedHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		switch h {
		case "":
		case "*":
			s.anyHost = true
		default:
			s.allowed[strings.Trim(h, "[]")] = true
		}
	}
	if !s.anyHost && len(s.allowed) == 0 {
		return nil, errors.New("dashboard: no allowed host")
	}

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, err
	}
	s.mux.HandleFunc("GET /api/overview", s.handle(func(ctx context.Context) (any, error) { return s.store.Overview(ctx) }))
	s.mux.HandleFunc("GET /api/secrets", s.handle(func(ctx context.Context) (any, error) { return s.store.Secrets(ctx) }))
	s.mux.HandleFunc("GET /api/pods", s.handle(func(ctx context.Context) (any, error) { return s.store.Pods(ctx) }))
	s.mux.HandleFunc("GET /api/violations", s.handle(func(ctx context.Context) (any, error) { return s.store.Violations(ctx) }))
	s.mux.HandleFunc("POST /api/validate", s.handleValidate)
	// Other methods on the routes above get the mux's 405. GET /api/validate
	// would otherwise fall through to the /api/ catch-all below.
	s.mux.HandleFunc("GET /api/validate", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
	})
	s.mux.HandleFunc("GET /api/", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "not found"})
	})
	s.mux.Handle("GET /", http.FileServerFS(static))
	return s, nil
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy",
		"default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; "+
			"base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")

	if !s.hostAllowed(r.Host) {
		writeJSON(w, http.StatusMisdirectedRequest, ErrorResponse{Error: "host not allowed (see --allowed-hosts)"})
		return
	}
	s.mux.ServeHTTP(w, r)
}

func (s *Server) hostAllowed(hostport string) bool {
	if s.anyHost {
		return true
	}
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	return host != "" && s.allowed[host]
}

func (s *Server) handle(get func(context.Context) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		data, err := get(ctx)
		if err != nil {
			s.opts.Log.Warnw("dashboard API request failed", "path", r.URL.Path, "error", err)
			writeJSON(w, http.StatusBadGateway, ErrorResponse{Error: err.Error()})
			return
		}
		s.writeData(w, data)
	}
}

func (s *Server) writeData(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, Envelope{Demo: s.opts.Demo, GeneratedAt: s.now().UTC(), Data: data})
}

// handleValidate returns the /validate-secrets verdict on the posted Secret
// YAML. The body holds a real secret value: it is never logged, stored or
// echoed, and parse errors are reduced to their line number.
func (s *Server) handleValidate(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, maxValidateBody)
	var buf strings.Builder
	if _, err := io.Copy(&buf, body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSON(w, http.StatusRequestEntityTooLarge, ErrorResponse{Error: fmt.Sprintf("the Secret YAML must be at most %d KiB", maxValidateBody>>10)})
			return
		}
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "could not read the request body"})
		return
	}
	secret, err := parseSecretYAML(buf.String())
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}
	verdict := webhook.ValidateSecret(secret)
	s.writeData(w, ValidateResult{
		Allowed: verdict.Allowed,
		Enabled: secret.Labels[webhook.LabelEnabled] == "true",
		Message: verdict.Message,
	})
}

var yamlLine = regexp.MustCompile(`line \d+`)

// parseSecretYAML decodes one Secret manifest. Its errors never quote the
// input.
func parseSecretYAML(doc string) (*corev1.Secret, error) {
	docs := splitYAMLDocuments(doc)
	switch len(docs) {
	case 0:
		return nil, errors.New("paste a Secret manifest")
	case 1:
	default:
		return nil, errors.New("paste a single Secret manifest (found several YAML documents)")
	}
	// Check the kind first, so another object's fields don't produce a
	// confusing Secret decoding error.
	var typeMeta metav1.TypeMeta
	if err := yaml.Unmarshal([]byte(docs[0]), &typeMeta); err != nil {
		return nil, sanitizeYAMLError(err)
	}
	if typeMeta.Kind != "" && typeMeta.Kind != "Secret" {
		return nil, fmt.Errorf("expected kind Secret, got %q", typeMeta.Kind)
	}
	if typeMeta.APIVersion != "" && typeMeta.APIVersion != "v1" {
		return nil, fmt.Errorf("expected apiVersion v1, got %q", typeMeta.APIVersion)
	}
	var secret corev1.Secret
	if err := yaml.Unmarshal([]byte(docs[0]), &secret); err != nil {
		return nil, sanitizeYAMLError(err)
	}
	return &secret, nil
}

// sanitizeYAMLError keeps only the line number of a decoding error: the
// decoder may quote the input, which holds secret values.
func sanitizeYAMLError(err error) error {
	msg := "invalid Secret YAML"
	if loc := yamlLine.FindString(err.Error()); loc != "" {
		msg += " (" + loc + ")"
	}
	if strings.Contains(err.Error(), "base64") {
		msg += ": data values must be base64-encoded (use stringData for plain text)"
	}
	return errors.New(msg)
}

// splitYAMLDocuments splits a YAML stream on "---" lines, dropping empty
// documents.
func splitYAMLDocuments(stream string) []string {
	var docs []string
	var cur []string
	flush := func() {
		d := strings.Join(cur, "\n")
		if strings.TrimSpace(stripYAMLComments(d)) != "" {
			docs = append(docs, d)
		}
		cur = cur[:0]
	}
	for _, line := range strings.Split(strings.ReplaceAll(stream, "\r\n", "\n"), "\n") {
		if strings.TrimRight(line, " \t") == "---" {
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return docs
}

func stripYAMLComments(doc string) string {
	var b strings.Builder
	for _, line := range strings.Split(doc, "\n") {
		if t := strings.TrimSpace(line); t != "" && !strings.HasPrefix(t, "#") {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

// Serve runs handler on addr until ctx is done, then shuts down gracefully.
func Serve(ctx context.Context, addr string, handler http.Handler, log *zap.SugaredLogger) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      requestTimeout + 10*time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    32 << 10,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	log.Infow("dashboard listening", "address", ln.Addr().String())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down dashboard server: %w", err)
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
