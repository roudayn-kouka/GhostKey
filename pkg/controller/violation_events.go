package controller

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"

	"github.com/roudayna/ghostkey/pkg/ebpf"
	gkevents "github.com/roudayna/ghostkey/pkg/events"
)

// ViolationEventRecorder publishes the data plane's binding and SNI
// violations as Warning Events regarding the writing pod, so they show up in
// `kubectl describe pod`, `kubectl get events` and the dashboard. It
// implements ebpf.ViolationRecorder; the manager rate-limits what it hands
// over exactly like the controller log.
type ViolationEventRecorder struct {
	recorder events.EventRecorder
}

// NewViolationEventRecorder returns a recorder publishing through rec,
// typically mgr.GetEventRecorder(gkevents.ReportingController).
func NewViolationEventRecorder(rec events.EventRecorder) *ViolationEventRecorder {
	return &ViolationEventRecorder{recorder: rec}
}

// RecordViolation implements ebpf.ViolationRecorder.
func (r *ViolationEventRecorder) RecordViolation(v *ebpf.Violation) {
	if v.PodUID == "" || v.PodName == "" {
		return
	}
	pod := &corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: v.PodNamespace,
			Name:      v.PodName,
			UID:       types.UID(v.PodUID),
		},
	}
	action := gkevents.ActionRewriteRefused
	if v.Audit {
		action = gkevents.ActionRewriteAllowed
	}
	r.recorder.Eventf(pod, nil, corev1.EventTypeWarning, v.Reason, action, "%s", v.Message())
}
