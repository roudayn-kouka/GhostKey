package controller

import (
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/roudayna/ghostkey/pkg/ebpf"
	gkevents "github.com/roudayna/ghostkey/pkg/events"
)

type recordedEvent struct {
	regarding                       *corev1.Pod
	eventType, reason, action, note string
}

type captureRecorder struct{ events []recordedEvent }

func (c *captureRecorder) Eventf(regarding, _ runtime.Object, eventType, reason, action, note string, args ...any) {
	pod, _ := regarding.(*corev1.Pod)
	c.events = append(c.events, recordedEvent{pod, eventType, reason, action, fmt.Sprintf(note, args...)})
}

func TestViolationEventRecorder(t *testing.T) {
	capture := &captureRecorder{}
	rec := NewViolationEventRecorder(capture)

	rec.RecordViolation(&ebpf.Violation{
		Reason: gkevents.ReasonSecretBindingViolation, PodUID: "uid", PodNamespace: "ns", PodName: "web",
		Secret: "ns/api[100%]",
	})
	rec.RecordViolation(&ebpf.Violation{
		Reason: gkevents.ReasonSNICheckViolation, Audit: true, PodUID: "uid", PodNamespace: "ns", PodName: "web",
		Secret: "ns/api[token]", SNIReason: "ClientHello without SNI",
	})
	rec.RecordViolation(&ebpf.Violation{Reason: gkevents.ReasonSecretBindingViolation}) // no pod: dropped

	if len(capture.events) != 2 {
		t.Fatalf("got %d events, want 2: %+v", len(capture.events), capture.events)
	}
	binding, sni := capture.events[0], capture.events[1]
	if binding.regarding == nil || binding.regarding.Namespace != "ns" || binding.regarding.Name != "web" || binding.regarding.UID != "uid" {
		t.Errorf("binding event regards %+v, want pod ns/web uid", binding.regarding)
	}
	if binding.eventType != corev1.EventTypeWarning || binding.reason != gkevents.ReasonSecretBindingViolation ||
		binding.action != gkevents.ActionRewriteRefused || !strings.Contains(binding.note, "ns/api[100%]") {
		t.Errorf("binding event = %+v", binding)
	}
	if sni.reason != gkevents.ReasonSNICheckViolation || sni.action != gkevents.ActionRewriteAllowed ||
		!strings.Contains(sni.note, "audit mode") {
		t.Errorf("SNI event = %+v", sni)
	}
}
