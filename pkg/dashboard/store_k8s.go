package dashboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	gkevents "github.com/roudayna/ghostkey/pkg/events"
	"github.com/roudayna/ghostkey/pkg/secrets"
	k8ssecrets "github.com/roudayna/ghostkey/pkg/secrets/k8s"
)

// annotationPodEnabled is the pod annotation the webhook stamps and the
// controller watches (pkg/controller.AnnotationEnabled).
const annotationPodEnabled = "ghostkey.io/enabled"

// maxViolations bounds the violations the API returns, newest first.
const maxViolations = 500

// KubeOptions locate GhostKey's components in the cluster. The defaults
// match the Helm chart.
type KubeOptions struct {
	// Namespace is where the controller and webhook run.
	Namespace           string
	ControllerDaemonSet string
	WebhookDeployment   string
	MutatingWebhook     string
	ValidatingWebhook   string
	// CacheTTL is how long a listing is reused across requests.
	CacheTTL time.Duration
}

// DefaultKubeOptions returns the Helm chart's names in namespace.
func DefaultKubeOptions(namespace string) *KubeOptions {
	return &KubeOptions{
		Namespace:           namespace,
		ControllerDaemonSet: "ghostkey-controller",
		WebhookDeployment:   "ghostkey-webhook",
		MutatingWebhook:     "ghostkey-mutating-webhook",
		ValidatingWebhook:   "ghostkey-validating-webhook",
		CacheTTL:            5 * time.Second,
	}
}

// KubeStore reads the cluster state through a controller-runtime reader.
// Pass an uncached reader (mgr.GetAPIReader()): a cached client would keep
// every Secret of the cluster, values included, in the dashboard's memory.
type KubeStore struct {
	reader client.Reader
	opts   KubeOptions

	inventory  *ttlCache[*inventory]
	overview   *ttlCache[*Overview]
	violations *ttlCache[[]Violation]
}

// NewKubeStore returns a Store over reader.
func NewKubeStore(reader client.Reader, opts *KubeOptions) *KubeStore {
	return &KubeStore{
		reader:     reader,
		opts:       *opts,
		inventory:  newTTLCache[*inventory](opts.CacheTTL),
		overview:   newTTLCache[*Overview](opts.CacheTTL),
		violations: newTTLCache[[]Violation](opts.CacheTTL),
	}
}

// inventory is the joined view of protected secrets and tracked pods.
type inventory struct {
	secrets []ProtectedSecret
	pods    []TrackedPod
}

// Secrets implements Store.
func (s *KubeStore) Secrets(ctx context.Context) ([]ProtectedSecret, error) {
	inv, err := s.inventory.get(ctx, s.fetchInventory)
	if err != nil {
		return nil, err
	}
	return inv.secrets, nil
}

// Pods implements Store.
func (s *KubeStore) Pods(ctx context.Context) ([]TrackedPod, error) {
	inv, err := s.inventory.get(ctx, s.fetchInventory)
	if err != nil {
		return nil, err
	}
	return inv.pods, nil
}

// Violations implements Store.
func (s *KubeStore) Violations(ctx context.Context) ([]Violation, error) {
	return s.violations.get(ctx, s.fetchViolations)
}

// Overview implements Store.
func (s *KubeStore) Overview(ctx context.Context) (*Overview, error) {
	return s.overview.get(ctx, s.fetchOverview)
}

func (s *KubeStore) fetchInventory(ctx context.Context) (*inventory, error) {
	// Real secrets as metadata only: their data never reaches the dashboard.
	enabled := &metav1.PartialObjectMetadataList{}
	enabled.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("SecretList"))
	if err := s.reader.List(ctx, enabled, client.MatchingLabels{k8ssecrets.LabelEnabled: "true"}); err != nil {
		return nil, fmt.Errorf("list ghostkey-enabled secrets: %w", err)
	}
	shadows, err := s.listShadows(ctx)
	if err != nil {
		return nil, err
	}
	var podList corev1.PodList
	if err := s.reader.List(ctx, &podList); err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}

	inv := &inventory{secrets: []ProtectedSecret{}, pods: []TrackedPod{}}
	podsBySecret := make(map[string][]PodRef)
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pod.Annotations[annotationPodEnabled] != "true" {
			continue
		}
		tp := TrackedPod{
			Namespace: pod.Namespace,
			Name:      pod.Name,
			Node:      pod.Spec.NodeName,
			Phase:     string(pod.Status.Phase),
			Ready:     podReady(pod),
			Secrets:   []BoundSecret{},
			CreatedAt: pod.CreationTimestamp.Time,
		}
		for _, ref := range k8ssecrets.PodSecretRefs(pod) {
			keys := ref.Keys
			if keys == nil {
				keys = []string{}
			}
			tp.Secrets = append(tp.Secrets, BoundSecret{Secret: ref.OwnerID, Keys: keys})
			podsBySecret[ref.OwnerID] = append(podsBySecret[ref.OwnerID], PodRef{Namespace: pod.Namespace, Name: pod.Name})
		}
		inv.pods = append(inv.pods, tp)
	}

	for i := range enabled.Items {
		meta := &enabled.Items[i]
		shadowName := meta.Name + k8ssecrets.ShadowSecretSuffix
		ps := ProtectedSecret{
			Namespace: meta.Namespace,
			Name:      meta.Name,
			Hosts:     meta.Annotations[k8ssecrets.AnnotationHosts],
			Port:      meta.Annotations[k8ssecrets.AnnotationPort],
			Problems:  []string{},
			Pods:      podsBySecret[meta.Namespace+"/"+meta.Name],
			CreatedAt: meta.CreationTimestamp.Time,
		}
		if ps.Pods == nil {
			ps.Pods = []PodRef{}
		}
		if sh, ok := shadows[types.NamespacedName{Namespace: meta.Namespace, Name: shadowName}]; ok {
			ps.Shadow = sh
		} else {
			ps.Shadow = ShadowInfo{Name: shadowName, Keys: []ShadowKey{}}
			ps.Problems = append(ps.Problems, fmt.Sprintf(
				"shadow secret %s does not exist: the webhook rejects pods that reference this secret", shadowName))
		}
		for _, k := range ps.Shadow.Keys {
			if k.Prefix == "" {
				ps.Problems = append(ps.Problems, fmt.Sprintf("placeholder of key %q does not start with %q", k.Key, secrets.ValuePrefix))
			}
		}
		if misplaced := k8ssecrets.MisplacedFilterLabels(meta.Labels); len(misplaced) > 0 {
			ps.Problems = append(ps.Problems, fmt.Sprintf(
				"%s set as a label instead of an annotation: the controller skips this secret", strings.Join(misplaced, ", ")))
		}
		inv.secrets = append(inv.secrets, ps)
	}

	sort.Slice(inv.secrets, func(i, j int) bool {
		return nsLess(inv.secrets[i].Namespace, inv.secrets[i].Name, inv.secrets[j].Namespace, inv.secrets[j].Name)
	})
	sort.Slice(inv.pods, func(i, j int) bool {
		return nsLess(inv.pods[i].Namespace, inv.pods[i].Name, inv.pods[j].Namespace, inv.pods[j].Name)
	})
	return inv, nil
}

// listShadows returns the shadow Secrets, reduced to key names, value
// lengths and whether each value carries the "kl::" prefix. The placeholders
// themselves are dropped here.
func (s *KubeStore) listShadows(ctx context.Context) (map[types.NamespacedName]ShadowInfo, error) {
	var list corev1.SecretList
	if err := s.reader.List(ctx, &list, client.MatchingLabels{k8ssecrets.LabelManaged: "true"}); err != nil {
		return nil, fmt.Errorf("list shadow secrets: %w", err)
	}
	out := make(map[types.NamespacedName]ShadowInfo, len(list.Items))
	for i := range list.Items {
		sec := &list.Items[i]
		info := ShadowInfo{Name: sec.Name, Exists: true, Managed: true, Keys: make([]ShadowKey, 0, len(sec.Data))}
		for key, value := range sec.Data {
			sk := ShadowKey{Key: key, Length: len(value)}
			if strings.HasPrefix(string(value), secrets.ValuePrefix) {
				sk.Prefix = secrets.ValuePrefix
			}
			info.Keys = append(info.Keys, sk)
		}
		sort.Slice(info.Keys, func(a, b int) bool { return info.Keys[a].Key < info.Keys[b].Key })
		out[types.NamespacedName{Namespace: sec.Namespace, Name: sec.Name}] = info
	}
	return out, nil
}

func (s *KubeStore) fetchOverview(ctx context.Context) (*Overview, error) {
	ov := &Overview{
		Namespace:  s.opts.Namespace,
		Controller: ControllerStatus{Nodes: []NodeController{}},
		Modes:      parseControllerModes(nil),
	}
	s.controllerStatus(ctx, ov)
	s.webhookStatus(ctx, &ov.Webhook)

	inv, err := s.inventory.get(ctx, s.fetchInventory)
	if err != nil {
		return nil, err
	}
	ov.Counts.ProtectedSecrets = len(inv.secrets)
	ov.Counts.TrackedPods = len(inv.pods)
	for i := range inv.secrets {
		if !inv.secrets[i].Shadow.Exists {
			ov.Counts.MissingShadows++
		}
	}
	// A failure to list Events is not worth failing the overview for.
	if v, err := s.violations.get(ctx, s.fetchViolations); err == nil {
		ov.Counts.Violations = len(v)
	}
	return ov, nil
}

// controllerStatus fills the controller DaemonSet, its pods and its modes.
func (s *KubeStore) controllerStatus(ctx context.Context, ov *Overview) {
	var ds appsv1.DaemonSet
	key := types.NamespacedName{Namespace: s.opts.Namespace, Name: s.opts.ControllerDaemonSet}
	if err := s.reader.Get(ctx, key, &ds); err != nil {
		ov.Controller.Error = describeGetError("DaemonSet", key, err)
		return
	}
	ov.Controller.Found = true
	ov.Controller.DesiredNodes = ds.Status.DesiredNumberScheduled
	ov.Controller.ReadyNodes = ds.Status.NumberReady
	if c := controllerContainer(ds.Spec.Template.Spec.Containers); c != nil {
		ov.Controller.Image = c.Image
		ov.Modes = parseControllerModes(append(append([]string{}, c.Command...), c.Args...))
		ov.Modes.FromDaemonSet = true
	}

	selector, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
	if err != nil || selector.Empty() {
		// An empty selector would list every pod of the namespace.
		return
	}
	var pods corev1.PodList
	if err := s.reader.List(ctx, &pods, client.InNamespace(s.opts.Namespace),
		client.MatchingLabelsSelector{Selector: selector}); err != nil {
		ov.Controller.Error = fmt.Sprintf("list controller pods: %v", err)
		return
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		nc := NodeController{Node: p.Spec.NodeName, Pod: p.Name, Phase: string(p.Status.Phase), Ready: podReady(p)}
		for j := range p.Status.ContainerStatuses {
			nc.Restarts += p.Status.ContainerStatuses[j].RestartCount
		}
		if p.Status.StartTime != nil {
			nc.StartedAt = p.Status.StartTime.Time
		}
		ov.Controller.Nodes = append(ov.Controller.Nodes, nc)
	}
	sort.Slice(ov.Controller.Nodes, func(i, j int) bool { return ov.Controller.Nodes[i].Node < ov.Controller.Nodes[j].Node })
}

// controllerContainer is the DaemonSet container running `ghostkey
// controller`: the one named "controller", else the first.
func controllerContainer(cs []corev1.Container) *corev1.Container {
	for i := range cs {
		if cs[i].Name == "controller" {
			return &cs[i]
		}
	}
	if len(cs) > 0 {
		return &cs[0]
	}
	return nil
}

func (s *KubeStore) webhookStatus(ctx context.Context, ws *WebhookStatus) {
	var dep appsv1.Deployment
	key := types.NamespacedName{Namespace: s.opts.Namespace, Name: s.opts.WebhookDeployment}
	if err := s.reader.Get(ctx, key, &dep); err != nil {
		ws.Error = describeGetError("Deployment", key, err)
	} else {
		ws.Found = true
		ws.Replicas = 1
		if dep.Spec.Replicas != nil {
			ws.Replicas = *dep.Spec.Replicas
		}
		ws.ReadyReplicas = dep.Status.ReadyReplicas
	}
	var mwc admissionregistrationv1.MutatingWebhookConfiguration
	ws.MutatingConfigured = s.reader.Get(ctx, types.NamespacedName{Name: s.opts.MutatingWebhook}, &mwc) == nil
	var vwc admissionregistrationv1.ValidatingWebhookConfiguration
	ws.ValidatingConfigured = s.reader.Get(ctx, types.NamespacedName{Name: s.opts.ValidatingWebhook}, &vwc) == nil
}

func (s *KubeStore) fetchViolations(ctx context.Context) ([]Violation, error) {
	out := []Violation{}
	for _, reason := range []string{gkevents.ReasonSecretBindingViolation, gkevents.ReasonSNICheckViolation} {
		var list corev1.EventList
		if err := s.reader.List(ctx, &list, client.MatchingFields{"reason": reason}); err != nil {
			return nil, fmt.Errorf("list %s events: %w", reason, err)
		}
		for i := range list.Items {
			e := &list.Items[i]
			// The field selector already matched the reason; only keep the
			// Events GhostKey's controller reported, about pods.
			if !gkevents.IsViolation(e.Reason) || e.ReportingController != gkevents.ReportingController ||
				e.InvolvedObject.Kind != "Pod" {
				continue
			}
			out = append(out, eventToViolation(e))
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	if len(out) > maxViolations {
		out = out[:maxViolations]
	}
	return out, nil
}

func eventToViolation(e *corev1.Event) Violation {
	count := e.Count
	if e.Series != nil {
		count = e.Series.Count
	}
	if count < 1 {
		count = 1
	}
	reporter := e.ReportingInstance
	if reporter == "" {
		reporter = e.Source.Host
	}
	return Violation{
		Time:      eventTime(e),
		Reason:    e.Reason,
		Action:    e.Action,
		Audit:     e.Action == gkevents.ActionRewriteAllowed,
		Namespace: e.InvolvedObject.Namespace,
		Pod:       e.InvolvedObject.Name,
		Message:   e.Message,
		Count:     count,
		Reporter:  reporter,
	}
}

// eventTime is the last time an Event was observed, across the fields the
// core/v1 and events.k8s.io/v1 APIs fill.
func eventTime(e *corev1.Event) time.Time {
	t := e.CreationTimestamp.Time
	for _, c := range []time.Time{e.EventTime.Time, e.LastTimestamp.Time, e.FirstTimestamp.Time} {
		if c.After(t) {
			t = c
		}
	}
	if e.Series != nil && e.Series.LastObservedTime.After(t) {
		t = e.Series.LastObservedTime.Time
	}
	return t
}

func podReady(p *corev1.Pod) bool {
	for _, c := range p.Status.Conditions {
		if c.Type == corev1.PodReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

func describeGetError(kind string, key types.NamespacedName, err error) string {
	switch {
	case apierrors.IsNotFound(err):
		return fmt.Sprintf("%s %s not found", kind, key)
	case apierrors.IsForbidden(err):
		return fmt.Sprintf("not allowed to read %s %s (check the dashboard's RBAC)", kind, key)
	default:
		return fmt.Sprintf("read %s %s: %v", kind, key, err)
	}
}

func nsLess(ns1, n1, ns2, n2 string) bool {
	if ns1 != ns2 {
		return ns1 < ns2
	}
	return n1 < n2
}

// Interface guard.
var _ Store = (*KubeStore)(nil)
