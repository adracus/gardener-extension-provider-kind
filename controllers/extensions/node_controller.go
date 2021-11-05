package extensions

import (
	"context"
	"fmt"

	"github.com/gardener/gardener/extensions/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	"k8s.io/apimachinery/pkg/runtime"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NodeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=extensions.gardener.cloud,resources=workers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=extensions.gardener.cloud,resources=workers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;update;patch

func (r *NodeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	return r.reconcile(ctx, log, pod)
}

func (r *NodeReconciler) SetupWithManager(mgr manager.Manager) error {
	workerPodSelector := metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
		Key:      "worker",
		Operator: metav1.LabelSelectorOpExists,
	}}}
	selectorPredicate, err := predicate.LabelSelectorPredicate(workerPodSelector)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{},
			builder.WithPredicates(
				selectorPredicate,
			),
		).
		Complete(r)
}

func isPodReady(pod *corev1.Pod) bool {
	for _, podCondition := range pod.Status.Conditions {
		if podCondition.Type == corev1.PodReady && podCondition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *NodeReconciler) reconcile(ctx context.Context, log logr.Logger, pod *corev1.Pod) (ctrl.Result, error) {
	if !isPodReady(pod) {
		return ctrl.Result{}, nil
	}

	worker := &extensionsv1alpha1.Worker{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Annotations["cluster-name"]}, worker); err != nil {
		return ctrl.Result{}, err
	}

	_, shootClient, err := util.NewClientForShoot(ctx, r.Client, pod.Namespace, client.Options{Scheme: r.Scheme})
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.patchVPNShootServiceLoadBalancer(ctx, shootClient, pod); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.patchNodeLabels(ctx, shootClient, pod, worker)
}

func (r *NodeReconciler) patchNodeLabels(ctx context.Context, shootClient client.Client, pod *corev1.Pod, worker *extensionsv1alpha1.Worker) error {
	node := &corev1.Node{}
	if err := shootClient.Get(ctx, client.ObjectKeyFromObject(pod), node); err != nil {
		return err
	}

	patch := client.MergeFrom(node.DeepCopy())
	for _, pool := range worker.Spec.Pools {
		for key, label := range pool.Labels {
			metav1.SetMetaDataLabel(&node.ObjectMeta, key, label)
		}
	}

	return shootClient.Patch(ctx, node, patch)
}

func (r *NodeReconciler) patchVPNShootServiceLoadBalancer(ctx context.Context, shootClient client.Client, pod *corev1.Pod) error {
	vpnService := &corev1.Service{}
	if err := shootClient.Get(ctx, client.ObjectKey{Namespace: metav1.NamespaceSystem, Name: "vpn-shoot"}, vpnService); err != nil {
		return err
	}
	servicePatch := client.StrategicMergeFrom(vpnService.DeepCopy())
	for i, servicePort := range vpnService.Spec.Ports {
		if servicePort.Name == "openvpn" {
			servicePort.NodePort = 30123
			vpnService.Spec.Ports[i] = servicePort
		}
	}
	if err := shootClient.Patch(ctx, vpnService, servicePatch); err != nil {
		return err
	}

	statusPatch := client.MergeFrom(vpnService.DeepCopy())
	vpnService.Status.LoadBalancer = corev1.LoadBalancerStatus{
		Ingress: []corev1.LoadBalancerIngress{{
			Hostname: fmt.Sprintf("vpn-shoot.%s", pod.Namespace),
		}},
	}
	if err := shootClient.Status().Patch(ctx, vpnService, statusPatch); err != nil {
		return err
	}
	return nil
}
