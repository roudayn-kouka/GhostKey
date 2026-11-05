package ebpf

import (
	"strings"
	"testing"
	"time"

	"github.com/roudayna/ghostkey/pkg/events"
	"github.com/roudayna/ghostkey/pkg/secrets"
)

type recordedViolations []Violation

func (r *recordedViolations) RecordViolation(v *Violation) { *r = append(*r, *v) }

func TestNewViolation(t *testing.T) {
	const shadow = "kl::token-placeholder-0001"
	snapshot := []secrets.Secret{{OwnerID: "ns/api", Key: "token", Real: "real-token-value-01", Shadow: shadow}}
	bindings := map[string]podBinding{"uid-1": {pod: "ns/web", cgroupIDs: []uint64{3, 7}}}
	key := placeholderKeys(shadow)[0]

	binding := newViolation(&secretBindingEvent{CgroupID: 7, Key: key, Reason: secretEventBinding}, bindings, snapshot)
	want := Violation{Reason: events.ReasonSecretBindingViolation, PodUID: "uid-1", PodNamespace: "ns", PodName: "web", Secret: "ns/api[token]"}
	if binding != want {
		t.Errorf("binding violation = %+v, want %+v", binding, want)
	}

	ev := &secretBindingEvent{CgroupID: 3, Key: key, Reason: secretEventSNI, SNIStatus: 6, Audit: 1, MemoryBIO: 1}
	copy(ev.SNI[:], "evil.example")
	sni := newViolation(ev, bindings, snapshot)
	if sni.Reason != events.ReasonSNICheckViolation || !sni.Audit || !sni.MemoryBIO || sni.SNI != "evil.example" ||
		sni.SNIReason != "SNI does not match the secret's host" || sni.PodUID != "uid-1" {
		t.Errorf("SNI violation = %+v", sni)
	}

	untracked := newViolation(&secretBindingEvent{CgroupID: 99, Key: key}, bindings, snapshot)
	if untracked.PodUID != "" || untracked.PodName != "" {
		t.Errorf("untracked cgroup attributed to a pod: %+v", untracked)
	}
}

func TestViolationMessage(t *testing.T) {
	for _, tc := range []struct {
		v    Violation
		want []string
	}{
		{Violation{Reason: events.ReasonSecretBindingViolation, Secret: "ns/api[token]"},
			[]string{"secret binding violation (rewrite refused)", "ns/api[token]"}},
		{Violation{Reason: events.ReasonSecretBindingViolation, Audit: true, Secret: "ns/api[token]"},
			[]string{"audit mode, rewrite allowed"}},
		{Violation{Reason: events.ReasonSNICheckViolation, Secret: "ns/api[token]", SNIReason: "ClientHello without SNI"},
			[]string{"SNI check violation (rewrite refused)", "(ClientHello without SNI)"}},
		{Violation{Reason: events.ReasonSNICheckViolation, Secret: "ns/api[token]", SNIReason: "mismatch", SNI: "evil.example", MemoryBIO: true},
			[]string{`SNI "evil.example"`, "memory-BIO process"}},
	} {
		msg := tc.v.Message()
		for _, w := range tc.want {
			if !strings.Contains(msg, w) {
				t.Errorf("Message() = %q, want it to contain %q", msg, w)
			}
		}
	}
}

// The recorder must see exactly what the log sees: one report per
// (cgroup, placeholder, reason) per bindingLogInterval.
func TestViolationRateLimit(t *testing.T) {
	logged := map[bindingLogKey]time.Time{}
	ev := &secretBindingEvent{CgroupID: 1, Reason: secretEventBinding}
	other := &secretBindingEvent{CgroupID: 2, Reason: secretEventBinding}
	t0 := time.Unix(1000, 0)

	if !allowViolationLog(logged, newBindingLogKey(ev), t0) {
		t.Fatal("first report refused")
	}
	if allowViolationLog(logged, newBindingLogKey(ev), t0.Add(bindingLogInterval-time.Second)) {
		t.Fatal("repeat within the interval allowed")
	}
	if !allowViolationLog(logged, newBindingLogKey(other), t0.Add(time.Second)) {
		t.Fatal("another cgroup's report refused")
	}
	sni := *ev
	sni.Reason = secretEventSNI
	if !allowViolationLog(logged, newBindingLogKey(&sni), t0.Add(time.Second)) {
		t.Fatal("another reason's report refused")
	}
	// Past every entry's interval: ev is reported again, the rest expire.
	if !allowViolationLog(logged, newBindingLogKey(ev), t0.Add(bindingLogInterval+time.Second)) {
		t.Fatal("report after the interval refused")
	}
	if len(logged) != 1 {
		t.Errorf("expired entries kept: %d entries, want 1", len(logged))
	}
}

func TestRecordViolation(t *testing.T) {
	var rec recordedViolations
	recordViolation(nil, &Violation{PodUID: "u"}) // no recorder: no-op
	recordViolation(&rec, &Violation{Secret: "untracked"})
	recordViolation(&rec, &Violation{PodUID: "u", PodName: "web"})
	if len(rec) != 1 || rec[0].PodUID != "u" {
		t.Errorf("recorded %+v, want only the tracked pod's violation", rec)
	}
}
