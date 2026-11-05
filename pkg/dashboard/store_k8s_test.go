package dashboard

import (
	"context"
	"reflect"
	"testing"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gkevents "github.com/roudayna/ghostkey/pkg/events"
)

// Real values and placeholders of the fixture. Each one is distinctive so a
// leak anywhere in an API response is caught by a plain substring search.
const (
	realStripe   = "REAL-SENTINEL-stripe-4242424242"
	shadowStripe = "kl::SHADOWTAIL-stripe-aaaaaaa"
	realWebhook  = "REAL-SENTINEL-whsec-0123456789"
	shadowWebhk  = "kl::SHADOWTAIL-whsec-bbbbbbbb"
	realOrphan   = "REAL-SENTINEL-orphan-987654321"
	realPlain    = "REAL-SENTINEL-not-protected-1"
	namespace    = "ghostkey-system"
)

var fixtureValues = []string{realStripe, shadowStripe, realWebhook, shadowWebhk, realOrphan, realPlain}

func fixtureObjects() []client.Object {
	now := metav1.NewTime(time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC))
	enabled := map[string]string{"ghostkey.io/enabled": "true"}
	ready := corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}
	replicas := int32(2)
	selector := map[string]string{"app.kubernetes.io/name": "ghostkey", "app.kubernetes.io/component": "controller"}

	return []client.Object{
		// Protected secret with a shadow.
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "billing", Name: "stripe", Labels: enabled, CreationTimestamp: now,
				Annotations: map[string]string{"ghostkey.io/hosts": "api.stripe.com", "ghostkey.io/port": "443"}},
			Data: map[string][]byte{"api-key": []byte(realStripe), "webhook": []byte(realWebhook)},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "billing", Name: "stripe-ghostkey",
				Labels: map[string]string{"ghostkey.io/managed": "true", "ghostkey.io/owner": "stripe"}},
			Data: map[string][]byte{"api-key": []byte(shadowStripe), "webhook": []byte(shadowWebhk)},
		},
		// Protected secret whose shadow is missing, with a misplaced filter.
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "ml", Name: "orphan",
				Labels: map[string]string{"ghostkey.io/enabled": "true", "ghostkey.io/hosts": "api.openai.com"}},
			StringData: map[string]string{"token": realOrphan},
			Data:       map[string][]byte{"token": []byte(realOrphan)},
		},
		// Not protected: must not show up at all.
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: "billing", Name: "db"},
			Data:       map[string][]byte{"password": []byte(realPlain)},
		},
		// Tracked pods.
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "billing", Name: "checkout", Annotations: enabled, CreationTimestamp: now},
			Spec: corev1.PodSpec{NodeName: "node-a", Containers: []corev1.Container{{Name: "app", Env: []corev1.EnvVar{{
				Name: "KEY", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "stripe-ghostkey"}, Key: "api-key"}},
			}}}}},
			Status: ready,
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "billing", Name: "worker", Annotations: enabled},
			Spec: corev1.PodSpec{NodeName: "node-b", Volumes: []corev1.Volume{{Name: "s", VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: "stripe-ghostkey"}}}},
				Containers: []corev1.Container{{Name: "app"}}},
			Status: corev1.PodStatus{Phase: corev1.PodPending},
		},
		// Not tracked (labeled, but the webhook has not annotated it).
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "billing", Name: "untracked", Labels: enabled},
			Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
		},
		// Controller DaemonSet and its pods.
		&appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "ghostkey-controller"},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{MatchLabels: selector},
				Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
					Name: "controller", Image: "ghcr.io/roudayna/ghostkey:test",
					Command: []string{"/ghostkey", "controller"},
					Args:    []string{"--enable-ebpf=true", "--secret-binding=audit", "--sni-check", "enforce", "--host-resolution=virtual-ip"},
				}}}},
			},
			Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 2, NumberReady: 1},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "ghostkey-controller-b", Labels: selector},
			Spec:       corev1.PodSpec{NodeName: "node-b"},
			Status:     corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{RestartCount: 3}}},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "ghostkey-controller-a", Labels: selector},
			Spec:       corev1.PodSpec{NodeName: "node-a"},
			Status:     ready,
		},
		// Webhook.
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: "ghostkey-webhook"},
			Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
			Status:     appsv1.DeploymentStatus{ReadyReplicas: 2},
		},
		&admissionregistrationv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: "ghostkey-mutating-webhook"}},
		// Violation Events, plus look-alikes that must be ignored.
		violationEvent("e1", gkevents.ReasonSecretBindingViolation, gkevents.ActionRewriteRefused, gkevents.ReportingController,
			"Pod", now.Add(-time.Minute), "secret binding violation (rewrite refused): pod used secret billing/stripe[api-key]"),
		violationEvent("e2", gkevents.ReasonSNICheckViolation, gkevents.ActionRewriteAllowed, gkevents.ReportingController,
			"Pod", now.Time, "SNI check violation (audit mode, rewrite allowed)"),
		violationEvent("spoof", gkevents.ReasonSNICheckViolation, gkevents.ActionRewriteAllowed, "someone-else", "Pod", now.Time, "x"),
		violationEvent("other", "BackOff", "", gkevents.ReportingController, "Pod", now.Time, "x"),
	}
}

func violationEvent(name, reason, action, reporter, kind string, at time.Time, msg string) *corev1.Event {
	return &corev1.Event{
		ObjectMeta:          metav1.ObjectMeta{Namespace: "billing", Name: name},
		InvolvedObject:      corev1.ObjectReference{Kind: kind, Namespace: "billing", Name: "checkout"},
		Reason:              reason,
		Action:              action,
		Message:             msg,
		Type:                corev1.EventTypeWarning,
		ReportingController: reporter,
		ReportingInstance:   reporter + "-node-a",
		EventTime:           metav1.NewMicroTime(at),
	}
}

func newFakeReader(t *testing.T, objs ...client.Object) client.Reader {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		// The apiserver supports this field selector on Events natively.
		WithIndex(&corev1.Event{}, "reason", func(o client.Object) []string { return []string{o.(*corev1.Event).Reason} }).
		Build()
}

func newFixtureStore(t *testing.T) *KubeStore {
	return NewKubeStore(newFakeReader(t, fixtureObjects()...), DefaultKubeOptions(namespace))
}

func TestKubeStoreSecrets(t *testing.T) {
	got, err := newFixtureStore(t).Secrets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d secrets, want 2 (only ghostkey-enabled ones): %+v", len(got), got)
	}
	stripe, orphan := got[0], got[1]
	if stripe.Namespace != "billing" || stripe.Name != "stripe" || stripe.Hosts != "api.stripe.com" || stripe.Port != "443" {
		t.Errorf("stripe = %+v", stripe)
	}
	wantKeys := []ShadowKey{
		{Key: "api-key", Length: len(shadowStripe), Prefix: "kl::"},
		{Key: "webhook", Length: len(shadowWebhk), Prefix: "kl::"},
	}
	if !stripe.Shadow.Exists || stripe.Shadow.Name != "stripe-ghostkey" || !reflect.DeepEqual(stripe.Shadow.Keys, wantKeys) {
		t.Errorf("stripe shadow = %+v, want keys %+v", stripe.Shadow, wantKeys)
	}
	if len(stripe.Problems) != 0 {
		t.Errorf("stripe problems = %v", stripe.Problems)
	}
	wantPods := []PodRef{{Namespace: "billing", Name: "checkout"}, {Namespace: "billing", Name: "worker"}}
	if !reflect.DeepEqual(stripe.Pods, wantPods) {
		t.Errorf("stripe pods = %+v, want %+v", stripe.Pods, wantPods)
	}

	if orphan.Shadow.Exists || len(orphan.Shadow.Keys) != 0 || len(orphan.Problems) != 2 || len(orphan.Pods) != 0 {
		t.Errorf("orphan = %+v, want a missing shadow and a misplaced-label problem", orphan)
	}
}

func TestKubeStorePods(t *testing.T) {
	got, err := newFixtureStore(t).Pods(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []TrackedPod{
		{Namespace: "billing", Name: "checkout", Node: "node-a", Phase: "Running", Ready: true,
			Secrets:   []BoundSecret{{Secret: "billing/stripe", Keys: []string{"api-key"}}},
			CreatedAt: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC)},
		{Namespace: "billing", Name: "worker", Node: "node-b", Phase: "Pending",
			Secrets: []BoundSecret{{Secret: "billing/stripe", Keys: []string{}}}},
	}
	for i := range got {
		got[i].CreatedAt = got[i].CreatedAt.UTC()
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pods =\n%+v\nwant\n%+v", got, want)
	}
}

func TestKubeStoreOverview(t *testing.T) {
	ov, err := newFixtureStore(t).Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := ov.Controller
	if !c.Found || c.DesiredNodes != 2 || c.ReadyNodes != 1 || c.Image != "ghcr.io/roudayna/ghostkey:test" {
		t.Errorf("controller = %+v", c)
	}
	if len(c.Nodes) != 2 || c.Nodes[0].Node != "node-a" || !c.Nodes[0].Ready || c.Nodes[1].Restarts != 3 || c.Nodes[1].Ready {
		t.Errorf("controller nodes = %+v", c.Nodes)
	}
	wantModes := Modes{FromDaemonSet: true, EBPFEnabled: true, SecretBinding: "audit", SNICheck: "enforce", HostResolution: "virtual-ip", TCAttachMode: "auto"}
	if ov.Modes != wantModes {
		t.Errorf("modes = %+v, want %+v", ov.Modes, wantModes)
	}
	wantWebhook := WebhookStatus{Found: true, Replicas: 2, ReadyReplicas: 2, MutatingConfigured: true}
	if ov.Webhook != wantWebhook {
		t.Errorf("webhook = %+v, want %+v", ov.Webhook, wantWebhook)
	}
	wantCounts := Counts{ProtectedSecrets: 2, MissingShadows: 1, TrackedPods: 2, Violations: 2}
	if ov.Counts != wantCounts {
		t.Errorf("counts = %+v, want %+v", ov.Counts, wantCounts)
	}
}

func TestKubeStoreOverviewWithoutComponents(t *testing.T) {
	ov, err := NewKubeStore(newFakeReader(t), DefaultKubeOptions(namespace)).Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ov.Controller.Found || ov.Controller.Error == "" || ov.Webhook.Found || ov.Webhook.Error == "" {
		t.Errorf("missing components not reported: %+v", ov)
	}
	if ov.Modes.FromDaemonSet || ov.Modes.SecretBinding != "enforce" {
		t.Errorf("modes without a DaemonSet = %+v, want the defaults", ov.Modes)
	}
}

func TestKubeStoreViolations(t *testing.T) {
	got, err := newFixtureStore(t).Violations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d violations, want 2 (GhostKey's own, about pods): %+v", len(got), got)
	}
	if got[0].Reason != gkevents.ReasonSNICheckViolation || !got[0].Audit || got[1].Reason != gkevents.ReasonSecretBindingViolation || got[1].Audit {
		t.Errorf("violations not newest first or wrongly classified: %+v", got)
	}
	if got[1].Namespace != "billing" || got[1].Pod != "checkout" || got[1].Count != 1 || got[1].Reporter != gkevents.ReportingController+"-node-a" {
		t.Errorf("violation = %+v", got[1])
	}
}

func TestEventTimeAndCount(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := &corev1.Event{
		EventTime: metav1.NewMicroTime(base),
		Series:    &corev1.EventSeries{Count: 5, LastObservedTime: metav1.NewMicroTime(base.Add(time.Hour))},
	}
	v := eventToViolation(e)
	if !v.Time.Equal(base.Add(time.Hour)) || v.Count != 5 {
		t.Errorf("series event = %+v", v)
	}
	legacy := &corev1.Event{Count: 3, LastTimestamp: metav1.NewTime(base.Add(2 * time.Hour)), Source: corev1.EventSource{Host: "node-z"}}
	v = eventToViolation(legacy)
	if !v.Time.Equal(base.Add(2*time.Hour)) || v.Count != 3 || v.Reporter != "node-z" {
		t.Errorf("legacy event = %+v", v)
	}
}

func TestParseControllerModes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want Modes
	}{
		{"defaults", nil, Modes{SecretBinding: "enforce", SNICheck: "enforce", HostResolution: "dns-snoop", TCAttachMode: "auto"}},
		{"equals and space forms", []string{"--enable-ebpf", "--sni-check", "AUDIT", "-tc-attach-mode=clsact", "--secret-binding="},
			Modes{EBPFEnabled: true, SecretBinding: "enforce", SNICheck: "audit", HostResolution: "dns-snoop", TCAttachMode: "clsact"}},
		{"explicit false and unknown flags", []string{"--enable-ebpf=false", "--cgroup-path", "/x", "--host-resolution=virtual-ip"},
			Modes{SecretBinding: "enforce", SNICheck: "enforce", HostResolution: "virtual-ip", TCAttachMode: "auto"}},
		{"value missing at the end", []string{"--sni-check"},
			Modes{SecretBinding: "enforce", SNICheck: "enforce", HostResolution: "dns-snoop", TCAttachMode: "auto"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseControllerModes(tc.args); got != tc.want {
				t.Errorf("parseControllerModes(%q) = %+v, want %+v", tc.args, got, tc.want)
			}
		})
	}
}

func TestTTLCache(t *testing.T) {
	c := newTTLCache[int](time.Minute)
	now := time.Unix(0, 0)
	c.now = func() time.Time { return now }
	calls := 0
	fetch := func(context.Context) (int, error) { calls++; return calls, nil }

	for range 3 {
		if v, _ := c.get(context.Background(), fetch); v != 1 {
			t.Fatalf("cached value = %d, want 1", v)
		}
	}
	now = now.Add(time.Minute)
	if v, _ := c.get(context.Background(), fetch); v != 2 {
		t.Fatalf("value after the TTL = %d, want 2", v)
	}
}
