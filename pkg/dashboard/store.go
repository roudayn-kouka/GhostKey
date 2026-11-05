package dashboard

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Store is where the dashboard reads the cluster state from.
type Store interface {
	Overview(ctx context.Context) (*Overview, error)
	Secrets(ctx context.Context) ([]ProtectedSecret, error)
	Pods(ctx context.Context) ([]TrackedPod, error)
	Violations(ctx context.Context) ([]Violation, error)
}

// Controller flag names the overview reads from the DaemonSet args.
const (
	flagEnableEBPF     = "enable-ebpf"
	flagSecretBinding  = "secret-binding"
	flagSNICheck       = "sni-check"
	flagHostResolution = "host-resolution"
	flagTCAttachMode   = "tc-attach-mode"
)

// ControllerFlagDefaults are the defaults of the `ghostkey controller` flags
// the overview shows, used when the DaemonSet omits one. They must match the
// flag definitions in cmd/ghostkey/controller.go (a test there checks it).
var ControllerFlagDefaults = map[string]string{
	flagEnableEBPF:     "false",
	flagSecretBinding:  "enforce",
	flagSNICheck:       "enforce",
	flagHostResolution: "dns-snoop",
	flagTCAttachMode:   "auto",
}

// parseControllerModes reads the data-plane modes from the controller
// container's args: "--flag=value", "--flag value", single-dash forms, and a
// bare boolean "--enable-ebpf". Flags that are absent take their default.
func parseControllerModes(args []string) Modes {
	values := make(map[string]string, len(ControllerFlagDefaults))
	for k, v := range ControllerFlagDefaults {
		values[k] = v
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name := strings.TrimLeft(arg, "-")
		value, hasValue := "", false
		if n, v, ok := strings.Cut(name, "="); ok {
			name, value, hasValue = n, v, true
		}
		if _, known := ControllerFlagDefaults[name]; !known {
			continue
		}
		if !hasValue {
			if name == flagEnableEBPF {
				// Boolean flags only take their value with "=".
				value = "true"
			} else if i+1 < len(args) {
				i++
				value = args[i]
			}
		}
		values[name] = value
	}
	ebpfEnabled, _ := strconv.ParseBool(values[flagEnableEBPF])
	return Modes{
		EBPFEnabled:    ebpfEnabled,
		SecretBinding:  orDefault(values[flagSecretBinding], flagSecretBinding),
		SNICheck:       orDefault(values[flagSNICheck], flagSNICheck),
		HostResolution: orDefault(values[flagHostResolution], flagHostResolution),
		TCAttachMode:   orDefault(values[flagTCAttachMode], flagTCAttachMode),
	}
}

// orDefault mirrors the controller, which treats an empty mode as its
// default.
func orDefault(v, flag string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return ControllerFlagDefaults[flag]
	}
	return v
}

// ttlCache memoizes one value for ttl, so a page refresh or several open
// tabs don't each list the whole cluster. Errors are not cached.
type ttlCache[T any] struct {
	ttl time.Duration
	now func() time.Time

	mu      sync.Mutex
	value   T
	fetched time.Time
	valid   bool
}

func newTTLCache[T any](ttl time.Duration) *ttlCache[T] {
	return &ttlCache[T]{ttl: ttl, now: time.Now}
}

// get returns the cached value, or calls fetch when it is stale. Concurrent
// callers wait for one fetch.
func (c *ttlCache[T]) get(ctx context.Context, fetch func(context.Context) (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && c.now().Sub(c.fetched) < c.ttl {
		return c.value, nil
	}
	v, err := fetch(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	c.value, c.fetched, c.valid = v, c.now(), true
	return v, nil
}
