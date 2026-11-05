// Package events holds the Kubernetes Event reasons GhostKey publishes, shared
// by the controller that emits them and the dashboard that lists them.
package events

const (
	// ReasonSecretBindingViolation is set on a Warning Event regarding a pod
	// that sent the placeholder of a secret its spec does not reference.
	ReasonSecretBindingViolation = "SecretBindingViolation"
	// ReasonSNICheckViolation is set on a Warning Event regarding a pod that
	// sent the placeholder of a host-restricted secret on a connection whose
	// TLS SNI is not the secret's host.
	ReasonSNICheckViolation = "SNICheckViolation"

	// ActionRewriteRefused is the Event action when the data plane left the
	// placeholder unchanged (enforce mode).
	ActionRewriteRefused = "RewriteRefused"
	// ActionRewriteAllowed is the Event action when the data plane patched
	// the secret anyway (audit mode).
	ActionRewriteAllowed = "RewriteAllowed"

	// ReportingController is the reportingController of GhostKey's
	// events.k8s.io/v1 Events.
	ReportingController = "ghostkey.io/controller"
)

// IsViolation reports whether reason is one of GhostKey's violation reasons.
func IsViolation(reason string) bool {
	return reason == ReasonSecretBindingViolation || reason == ReasonSNICheckViolation
}
