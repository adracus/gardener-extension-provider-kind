package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// InfrastructureConfig is configuration for kind Infrastructure.
// +kubebuilder:object:root=true
type InfrastructureConfig struct {
	metav1.TypeMeta `json:",inline"`
}

// InfrastructureStatus is the status for kind Infrastructure.
// +kubebuilder:object:root=true
type InfrastructureStatus struct {
	metav1.TypeMeta `json:",inline"`
}

func init() {
	SchemeBuilder.Register(&InfrastructureConfig{}, &InfrastructureStatus{})
}
