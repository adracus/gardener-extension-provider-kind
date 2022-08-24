module github.com/gardener/gardener-extension-provider-kind

go 1.16

require (
	github.com/gardener/gardener v1.34.1
	github.com/go-logr/logr v1.2.3
	k8s.io/api v0.25.0
	k8s.io/apimachinery v0.25.0
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/cluster-bootstrap v0.25.0
	k8s.io/utils v0.0.0-20220728103510-ee6ede2d64ed
	sigs.k8s.io/controller-runtime v0.10.2
)

replace (
	k8s.io/api => k8s.io/api v0.22.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.22.3
	k8s.io/client-go => k8s.io/client-go v0.22.3
)
