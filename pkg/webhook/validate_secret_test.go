package webhook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"go.uber.org/zap"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// ValidateSecret is the webhook's verdict, message included, so the
// dashboard and the admission response can never disagree.
func TestValidateSecretMatchesHandle(t *testing.T) {
	enabled := map[string]string{LabelEnabled: "true"}
	valid := map[string][]byte{"api-key": []byte("super-secret-token")}
	cases := []struct {
		name        string
		labels      map[string]string
		annotations map[string]string
		data        map[string][]byte
		wantAllowed bool
		wantMessage string
	}{
		{"not enabled", nil, nil, nil, true, "not ghostkey-enabled"},
		{"valid", enabled, map[string]string{AnnotationHosts: "api.example.com", AnnotationPort: "443"}, valid, true, "valid"},
		{"filter as label", map[string]string{LabelEnabled: "true", AnnotationHosts: "api.example.com"}, nil, valid, false, "must be set as an annotation"},
		{"multi host", enabled, map[string]string{AnnotationHosts: "a.com,b.com"}, valid, false, "invalid ghostkey.io/hosts annotation"},
		{"bad port", enabled, map[string]string{AnnotationPort: "443/sctp"}, valid, false, "invalid ghostkey.io/port annotation"},
		{"too short", enabled, nil, map[string][]byte{"k": []byte("short")}, false, "minimum 8 required"},
		{"no data", enabled, nil, nil, false, "no data entries"},
	}
	v := NewSecretValidator(zap.NewNop().Sugar())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &corev1.Secret{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
				ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "default", Labels: tc.labels, Annotations: tc.annotations},
				Data:       tc.data,
			}
			got := ValidateSecret(s)
			if got.Allowed != tc.wantAllowed || !strings.Contains(got.Message, tc.wantMessage) {
				t.Fatalf("ValidateSecret = %+v, want allowed=%v message containing %q", got, tc.wantAllowed, tc.wantMessage)
			}

			raw, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			resp := v.Handle(context.Background(), admission.Request{AdmissionRequest: admissionv1.AdmissionRequest{
				Object: runtime.RawExtension{Raw: raw},
			}})
			if resp.Allowed != got.Allowed || resp.Result.Message != got.Message {
				t.Errorf("Handle = (%v, %q), ValidateSecret = (%v, %q)", resp.Allowed, resp.Result.Message, got.Allowed, got.Message)
			}
			if strings.Contains(got.Message, "super-secret-token") {
				t.Errorf("verdict leaks the secret value: %q", got.Message)
			}
		})
	}
}
