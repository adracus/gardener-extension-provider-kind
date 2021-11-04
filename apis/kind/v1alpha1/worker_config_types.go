package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	UserDataKey = "userdata"
)

// WorkerConfig is configuration for kind Infrastructure.
// +kubebuilder:object:root=true
type WorkerConfig struct {
	metav1.TypeMeta `json:",inline"`
}

func init() {
	SchemeBuilder.Register(&WorkerConfig{})
}
