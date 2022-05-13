module github.com/gardener/gardener-extension-provider-kind

go 1.16

require (
	github.com/gardener/gardener v1.34.1
	github.com/go-logr/logr v1.2.0
	k8s.io/api v0.24.0
	k8s.io/apimachinery v0.24.0
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/cluster-bootstrap v0.22.2
	k8s.io/utils v0.0.0-20220210201930-3a6ce19ff2f9
	sigs.k8s.io/controller-runtime v0.12.0
)

replace (
	k8s.io/api => k8s.io/api v0.22.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.22.3
	k8s.io/client-go => k8s.io/client-go v0.22.3
)
