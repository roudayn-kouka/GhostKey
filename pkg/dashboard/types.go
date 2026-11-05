// Package dashboard serves GhostKey's read-only web dashboard: a JSON API
// over the cluster state (controller, webhook, protected secrets, tracked
// pods, violations) and an embedded static frontend.
//
// The API never returns a secret value. The response types below carry
// names, keys, lengths and annotations only; a real Secret is only ever read
// as metadata, and of a shadow only the key names, value lengths and the
// constant "kl::" prefix are kept.
package dashboard

import "time"

// Envelope wraps every successful API response.
type Envelope struct {
	// Demo is true when the dashboard serves demonstration data
	// (--demo-data): the frontend shows a banner.
	Demo        bool      `json:"demo"`
	GeneratedAt time.Time `json:"generatedAt"`
	Data        any       `json:"data"`
}

// ErrorResponse is the body of a failed API request.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Overview is the /api/overview payload.
type Overview struct {
	Namespace  string           `json:"namespace"`
	Controller ControllerStatus `json:"controller"`
	Webhook    WebhookStatus    `json:"webhook"`
	Modes      Modes            `json:"modes"`
	Counts     Counts           `json:"counts"`
}

// ControllerStatus is the state of the controller DaemonSet.
type ControllerStatus struct {
	Found bool `json:"found"`
	// Error explains why the DaemonSet could not be read, if it could not.
	Error        string           `json:"error,omitempty"`
	Image        string           `json:"image,omitempty"`
	DesiredNodes int32            `json:"desiredNodes"`
	ReadyNodes   int32            `json:"readyNodes"`
	Nodes        []NodeController `json:"nodes"`
}

// NodeController is the controller pod of one node.
type NodeController struct {
	Node      string    `json:"node"`
	Pod       string    `json:"pod"`
	Phase     string    `json:"phase"`
	Ready     bool      `json:"ready"`
	Restarts  int32     `json:"restarts"`
	StartedAt time.Time `json:"startedAt,omitzero"`
}

// WebhookStatus is the state of the webhook Deployment and its admission
// configurations.
type WebhookStatus struct {
	Found                bool   `json:"found"`
	Error                string `json:"error,omitempty"`
	Replicas             int32  `json:"replicas"`
	ReadyReplicas        int32  `json:"readyReplicas"`
	MutatingConfigured   bool   `json:"mutatingConfigured"`
	ValidatingConfigured bool   `json:"validatingConfigured"`
}

// Modes are the data-plane modes the controller runs with, read from the
// DaemonSet's container args.
type Modes struct {
	// FromDaemonSet is false when the DaemonSet could not be read and the
	// values below are the controller's flag defaults.
	FromDaemonSet  bool   `json:"fromDaemonSet"`
	EBPFEnabled    bool   `json:"ebpfEnabled"`
	SecretBinding  string `json:"secretBinding"`
	SNICheck       string `json:"sniCheck"`
	HostResolution string `json:"hostResolution"`
	TCAttachMode   string `json:"tcAttachMode"`
}

// Counts are the headline numbers of the overview.
type Counts struct {
	ProtectedSecrets int `json:"protectedSecrets"`
	MissingShadows   int `json:"missingShadows"`
	TrackedPods      int `json:"trackedPods"`
	// Violations counts the violation Events the cluster still holds
	// (Events expire, by default after one hour).
	Violations int `json:"violations"`
}

// ProtectedSecret is a Secret labeled ghostkey.io/enabled=true.
type ProtectedSecret struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Hosts and Port are the ghostkey.io/hosts and ghostkey.io/port
	// annotations, empty when unset.
	Hosts string `json:"hosts"`
	Port  string `json:"port"`
	// Shadow is the state of the shadow Secret <name>-ghostkey.
	Shadow ShadowInfo `json:"shadow"`
	// Problems are configuration issues that leave the secret unprotected.
	Problems  []string  `json:"problems"`
	Pods      []PodRef  `json:"pods"`
	CreatedAt time.Time `json:"createdAt,omitzero"`
}

// ShadowInfo describes a shadow Secret without its values.
type ShadowInfo struct {
	Name   string `json:"name"`
	Exists bool   `json:"exists"`
	// Managed is true when the shadow carries ghostkey.io/managed=true.
	Managed bool        `json:"managed"`
	Keys    []ShadowKey `json:"keys"`
}

// ShadowKey is one data key of a shadow Secret: its name, the length of its
// placeholder (the real value has the same length) and whether the
// placeholder carries the "kl::" prefix. Nothing else of the value is kept.
type ShadowKey struct {
	Key    string `json:"key"`
	Length int    `json:"length"`
	// Prefix is "kl::" when the placeholder starts with it, "" otherwise.
	Prefix string `json:"prefix"`
}

// PodRef names a pod.
type PodRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// TrackedPod is a pod annotated ghostkey.io/enabled=true.
type TrackedPod struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Node      string `json:"node"`
	Phase     string `json:"phase"`
	Ready     bool   `json:"ready"`
	// Secrets are the protected secrets the pod references
	// (k8s.PodSecretRefs): the only ones it may redeem.
	Secrets   []BoundSecret `json:"secrets"`
	CreatedAt time.Time     `json:"createdAt,omitzero"`
}

// BoundSecret is a protected secret a pod references.
type BoundSecret struct {
	// Secret is "namespace/name" of the real Secret.
	Secret string `json:"secret"`
	// Keys are the referenced keys; empty means every key.
	Keys []string `json:"keys"`
}

// Violation is a SecretBindingViolation or SNICheckViolation Event.
type Violation struct {
	Time      time.Time `json:"time,omitzero"`
	Reason    string    `json:"reason"`
	Action    string    `json:"action"`
	Audit     bool      `json:"audit"`
	Namespace string    `json:"namespace"`
	Pod       string    `json:"pod"`
	Message   string    `json:"message"`
	Count     int32     `json:"count"`
	// Reporter is the controller instance (node) that reported it.
	Reporter string `json:"reporter"`
}

// ValidateResult is the /api/validate payload: the verdict the
// /validate-secrets admission webhook would return.
type ValidateResult struct {
	Allowed bool `json:"allowed"`
	// Enabled is false when the Secret lacks ghostkey.io/enabled=true (the
	// webhook lets it through untouched).
	Enabled bool   `json:"enabled"`
	Message string `json:"message"`
}
