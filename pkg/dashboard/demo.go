package dashboard

import (
	"context"
	"time"

	gkevents "github.com/roudayna/ghostkey/pkg/events"
)

// DemoStore serves fixed, fictitious cluster state for `ghostkey dashboard
// --demo-data`, so the UI can be shown and screenshotted without a cluster.
// Every response is flagged as demo data and the frontend shows a banner.
//
// TODO: demo-only fixture data. Replace it with a recorded snapshot of a
// real cluster (or drop the flag) once the dashboard has e2e coverage
// against the k3s suite.
type DemoStore struct {
	now func() time.Time
}

// NewDemoStore returns the demonstration Store.
func NewDemoStore() *DemoStore { return &DemoStore{now: time.Now} }

// Overview implements Store.
func (d *DemoStore) Overview(context.Context) (*Overview, error) {
	now := d.now()
	return &Overview{
		Namespace: "ghostkey-system",
		Controller: ControllerStatus{
			Found:        true,
			Image:        "ghcr.io/roudayna/ghostkey:v0.9.0",
			DesiredNodes: 3,
			ReadyNodes:   2,
			Nodes: []NodeController{
				{Node: "node-a", Pod: "ghostkey-controller-7xk2p", Phase: "Running", Ready: true, StartedAt: now.Add(-74 * time.Hour)},
				{Node: "node-b", Pod: "ghostkey-controller-q9d4m", Phase: "Running", Ready: true, Restarts: 1, StartedAt: now.Add(-26 * time.Hour)},
				{Node: "node-c", Pod: "ghostkey-controller-vt8zn", Phase: "Pending", StartedAt: now.Add(-3 * time.Minute)},
			},
		},
		Webhook: WebhookStatus{Found: true, Replicas: 2, ReadyReplicas: 2, MutatingConfigured: true, ValidatingConfigured: true},
		Modes: Modes{
			FromDaemonSet:  true,
			EBPFEnabled:    true,
			SecretBinding:  "enforce",
			SNICheck:       "audit",
			HostResolution: "dns-snoop",
			TCAttachMode:   "auto",
		},
		Counts: Counts{ProtectedSecrets: 4, MissingShadows: 1, TrackedPods: 4, Violations: 3},
	}, nil
}

// Secrets implements Store.
func (d *DemoStore) Secrets(context.Context) ([]ProtectedSecret, error) {
	now := d.now()
	return []ProtectedSecret{
		{
			Namespace: "billing", Name: "stripe-credentials", Hosts: "api.stripe.com", Port: "443",
			Shadow: ShadowInfo{Name: "stripe-credentials-ghostkey", Exists: true, Managed: true, Keys: []ShadowKey{
				{Key: "api-key", Length: 32, Prefix: "kl::"},
				{Key: "webhook-secret", Length: 38, Prefix: "kl::"},
			}},
			Problems:  []string{},
			Pods:      []PodRef{{Namespace: "billing", Name: "checkout-6d9f7c-2xlqk"}, {Namespace: "billing", Name: "checkout-6d9f7c-h8w2r"}},
			CreatedAt: now.Add(-30 * 24 * time.Hour),
		},
		{
			Namespace: "billing", Name: "sendgrid", Hosts: "api.sendgrid.com",
			Shadow: ShadowInfo{Name: "sendgrid-ghostkey", Exists: true, Managed: true, Keys: []ShadowKey{
				{Key: "token", Length: 69, Prefix: "kl::"},
			}},
			Problems:  []string{},
			Pods:      []PodRef{{Namespace: "billing", Name: "mailer-5b8c4d-k2j9s"}},
			CreatedAt: now.Add(-12 * 24 * time.Hour),
		},
		{
			Namespace: "ml", Name: "openai", Hosts: "api.openai.com", Port: "443/tcp",
			Shadow: ShadowInfo{Name: "openai-ghostkey", Exists: true, Managed: true, Keys: []ShadowKey{
				{Key: "OPENAI_API_KEY", Length: 51, Prefix: "kl::"},
			}},
			Problems:  []string{},
			Pods:      []PodRef{{Namespace: "ml", Name: "summarizer-0"}},
			CreatedAt: now.Add(-5 * 24 * time.Hour),
		},
		{
			Namespace: "ml", Name: "hf-token", Hosts: "huggingface.co",
			Shadow: ShadowInfo{Name: "hf-token-ghostkey", Keys: []ShadowKey{}},
			Problems: []string{
				"shadow secret hf-token-ghostkey does not exist: the webhook rejects pods that reference this secret",
			},
			Pods:      []PodRef{},
			CreatedAt: now.Add(-2 * time.Minute),
		},
	}, nil
}

// Pods implements Store.
func (d *DemoStore) Pods(context.Context) ([]TrackedPod, error) {
	now := d.now()
	return []TrackedPod{
		{Namespace: "billing", Name: "checkout-6d9f7c-2xlqk", Node: "node-a", Phase: "Running", Ready: true,
			Secrets: []BoundSecret{{Secret: "billing/stripe-credentials", Keys: []string{"api-key"}}}, CreatedAt: now.Add(-50 * time.Hour)},
		{Namespace: "billing", Name: "checkout-6d9f7c-h8w2r", Node: "node-b", Phase: "Running", Ready: true,
			Secrets: []BoundSecret{{Secret: "billing/stripe-credentials", Keys: []string{}}}, CreatedAt: now.Add(-50 * time.Hour)},
		{Namespace: "billing", Name: "mailer-5b8c4d-k2j9s", Node: "node-a", Phase: "Running", Ready: true,
			Secrets: []BoundSecret{{Secret: "billing/sendgrid", Keys: []string{"token"}}}, CreatedAt: now.Add(-9 * time.Hour)},
		{Namespace: "ml", Name: "summarizer-0", Node: "node-b", Phase: "Running", Ready: false,
			Secrets: []BoundSecret{{Secret: "ml/openai", Keys: []string{}}}, CreatedAt: now.Add(-40 * time.Minute)},
	}, nil
}

// Violations implements Store.
func (d *DemoStore) Violations(context.Context) ([]Violation, error) {
	now := d.now()
	return []Violation{
		{
			Time: now.Add(-4 * time.Minute), Reason: gkevents.ReasonSecretBindingViolation, Action: gkevents.ActionRewriteRefused,
			Namespace: "billing", Pod: "mailer-5b8c4d-k2j9s", Count: 7, Reporter: "ghostkey.io/controller-node-a",
			Message: "secret binding violation (rewrite refused): pod used secret billing/stripe-credentials[api-key], which its spec does not reference",
		},
		{
			Time: now.Add(-18 * time.Minute), Reason: gkevents.ReasonSNICheckViolation, Action: gkevents.ActionRewriteAllowed, Audit: true,
			Namespace: "ml", Pod: "summarizer-0", Count: 2, Reporter: "ghostkey.io/controller-node-b",
			Message: `SNI check violation (audit mode, rewrite allowed): secret ml/openai[OPENAI_API_KEY] was sent on a connection whose TLS SNI is not the secret's host (SNI does not match the secret's host, SNI "proxy.internal.example")`,
		},
		{
			Time: now.Add(-52 * time.Minute), Reason: gkevents.ReasonSNICheckViolation, Action: gkevents.ActionRewriteAllowed, Audit: true,
			Namespace: "billing", Pod: "checkout-6d9f7c-h8w2r", Count: 1, Reporter: "ghostkey.io/controller-node-b",
			Message: "SNI check violation (audit mode, rewrite allowed): secret billing/stripe-credentials[api-key] was sent on a connection whose TLS SNI is not the secret's host (ClientHello without SNI)",
		},
	}, nil
}

var _ Store = (*DemoStore)(nil)
