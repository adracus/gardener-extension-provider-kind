package extensions

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	"k8s.io/apimachinery/pkg/runtime"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	HostIP string
}

// +kubebuilder:rbac:groups=extensions.gardener.cloud,resources=workers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=extensions.gardener.cloud,resources=workers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;update;patch

func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	service := &corev1.Service{}
	if err := r.Get(ctx, req.NamespacedName, service); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return r.reconcile(ctx, log, service)
}

func (r *ServiceReconciler) SetupWithManager(mgr manager.Manager) error {
	serviceSelector := metav1.LabelSelector{MatchLabels: map[string]string{
		"app":  "kubernetes",
		"role": "apiserver",
	}}
	selectorPredicate, err := predicate.LabelSelectorPredicate(serviceSelector)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{},
			builder.WithPredicates(
				selectorPredicate,
			),
		).
		Complete(r)
}

func (r *ServiceReconciler) reconcile(ctx context.Context, log logr.Logger, service *corev1.Service) (ctrl.Result, error) {
	patch := client.StrategicMergeFrom(service.DeepCopy())
	for i, servicePort := range service.Spec.Ports {
		if servicePort.Name == "kube-apiserver" {
			servicePort.NodePort = 30443
			service.Spec.Ports[i] = servicePort
		}
	}
	if err := r.Patch(ctx, service, patch); err != nil {
		return ctrl.Result{}, err
	}

	statusPatch := client.MergeFrom(service.DeepCopy())
	service.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{{
		IP: r.HostIP,
	}}

	return ctrl.Result{}, r.Status().Patch(ctx, service, statusPatch)
}
