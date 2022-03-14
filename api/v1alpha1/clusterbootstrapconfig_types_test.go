package v1alpha1

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestControlPlaneWait(t *testing.T) {
	cfg := ClusterBootstrapConfig{}

	if v := cfg.ControlPlaneWait(); v != defaultWaitDuration {
		t.Fatalf("ControlPlaneWait() got %v, want %v", v, defaultWaitDuration)
	}

	want := time.Second * 20
	cfg.Spec.ControlPlaneWaitDuration = &metav1.Duration{Duration: want}
	if v := cfg.ControlPlaneWait(); v != want {
		t.Fatalf("ControlPlaneWait() got %v, want %v", v, want)
	}
}
