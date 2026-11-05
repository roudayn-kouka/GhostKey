package ebpf

import (
	"fmt"
	"strings"
	"time"

	"github.com/roudayna/ghostkey/pkg/events"
	"github.com/roudayna/ghostkey/pkg/secrets"
)

// Violation is a placeholder rewrite the data plane refused (or, in audit
// mode, allowed but flagged): a pod used a secret it does not reference, or
// a host-restricted secret on a connection whose SNI is not the host. It
// names the secret and the pod, never a value.
type Violation struct {
	// Reason is events.ReasonSecretBindingViolation or
	// events.ReasonSNICheckViolation.
	Reason string
	// Audit is true when the rewrite was allowed (audit mode).
	Audit bool
	// PodUID, PodNamespace and PodName identify the writing pod; PodUID is
	// empty when the cgroup belongs to no tracked pod.
	PodUID       string
	PodNamespace string
	PodName      string
	// Secret is "namespace/name[key]", or "unknown".
	Secret string
	// SNIReason and SNI describe an SNI violation: why the check failed and
	// the SNI the connection carried, if any.
	SNIReason string
	SNI       string
	// MemoryBIO is set for memory-BIO writers (Node.js, Bun), whose refused
	// socket may be another connection of the same process.
	MemoryBIO bool
}

// ViolationRecorder receives the violations the controller logs, after the
// same per-(cgroup, placeholder, reason) rate limiting as the log.
type ViolationRecorder interface {
	RecordViolation(*Violation)
}

// Message is a human-readable, value-free description of the violation.
func (v *Violation) Message() string {
	outcome := "rewrite refused"
	if v.Audit {
		outcome = "audit mode, rewrite allowed"
	}
	if v.Reason == events.ReasonSNICheckViolation {
		msg := fmt.Sprintf("SNI check violation (%s): secret %s was sent on a connection whose TLS SNI is not the secret's host (%s",
			outcome, v.Secret, v.SNIReason)
		if v.SNI != "" {
			msg += fmt.Sprintf(", SNI %q", v.SNI)
		}
		msg += ")"
		if v.MemoryBIO {
			msg += "; memory-BIO process: the refused socket may be another connection of the same process"
		}
		return msg
	}
	return fmt.Sprintf("secret binding violation (%s): pod used secret %s, which its spec does not reference", outcome, v.Secret)
}

// newViolation describes a violation event. bindings and snapshot are
// best-effort context, as for describeBindingEvent.
func newViolation(ev *secretBindingEvent, bindings map[string]podBinding, snapshot []secrets.Secret) Violation {
	_, secret := describeBindingEvent(ev, bindings, snapshot)
	v := Violation{
		Reason:    events.ReasonSecretBindingViolation,
		Audit:     ev.Audit == 1,
		Secret:    secret,
		MemoryBIO: ev.MemoryBIO == 1,
	}
	if ev.Reason == secretEventSNI {
		v.Reason = events.ReasonSNICheckViolation
		v.SNIReason, v.SNI = describeSNIEvent(ev)
	}
	if uid, b, ok := bindingOfCgroup(ev.CgroupID, bindings); ok {
		v.PodUID = uid
		v.PodNamespace, v.PodName, _ = strings.Cut(b.pod, "/")
	}
	return v
}

// bindingLogInterval bounds how often the same (cgroup, placeholder)
// violation is logged and recorded.
const bindingLogInterval = time.Minute

// bindingLogKey identifies a logged binding or SNI violation.
type bindingLogKey struct {
	acl       secretACLKey
	reason    uint8
	sniStatus uint8
}

// newBindingLogKey is the rate-limiting key of a violation event.
func newBindingLogKey(ev *secretBindingEvent) bindingLogKey {
	return bindingLogKey{
		acl:       secretACLKey{CgroupID: ev.CgroupID, Key: ev.Key},
		reason:    ev.Reason,
		sniStatus: ev.SNIStatus,
	}
}

// allowViolationLog reports whether a violation with key may be logged (and
// recorded) at now: at most once per bindingLogInterval. It records the
// report in logged and drops the entries that expired.
func allowViolationLog(logged map[bindingLogKey]time.Time, key bindingLogKey, now time.Time) bool {
	if last, ok := logged[key]; ok && now.Sub(last) < bindingLogInterval {
		return false
	}
	logged[key] = now
	for k, at := range logged {
		if now.Sub(at) >= bindingLogInterval {
			delete(logged, k)
		}
	}
	return true
}

// recordViolation hands v to rec, if any. A violation from a cgroup no
// tracked pod holds has nothing to be recorded against: only the log has it.
func recordViolation(rec ViolationRecorder, v *Violation) {
	if rec == nil || v.PodUID == "" {
		return
	}
	rec.RecordViolation(v)
}

// bindingOfCgroup returns the binding, and its pod UID, holding cgroupID.
func bindingOfCgroup(cgroupID uint64, bindings map[string]podBinding) (string, podBinding, bool) {
	for uid, b := range bindings {
		for _, cg := range b.cgroupIDs {
			if cg == cgroupID {
				return uid, b, true
			}
		}
	}
	return "", podBinding{}, false
}
