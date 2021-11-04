package extensions

import (
	"context"
	"fmt"

	gardenerpredicate "github.com/gardener/gardener/extensions/pkg/predicate"
	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	kindv1alpha1 "github.com/gardener/gardener-extension-provider-kind/apis/kind/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type InfrastructureReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=extensions.gardener.cloud,resources=infrastructures,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=extensions.gardener.cloud,resources=infrastructures/status,verbs=get;update;patch

func (r *InfrastructureReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	infra := &extensionsv1alpha1.Infrastructure{}
	if err := r.Get(ctx, req.NamespacedName, infra); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !infra.DeletionTimestamp.IsZero() {
		return r.delete(ctx, log, infra)
	}
	return r.reconcile(ctx, log, infra)
}

func (r *InfrastructureReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&extensionsv1alpha1.Infrastructure{},
			builder.WithPredicates(
				predicate.NewPredicateFuncs(func(object client.Object) bool {
					infra := object.(*extensionsv1alpha1.Infrastructure)
					return infra.Spec.Type == kindv1alpha1.ExtensionType
				}),
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					gardenerpredicate.HasOperationAnnotation(),
				),
			),
		).
		Complete(r)
}

func (r *InfrastructureReconciler) delete(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *InfrastructureReconciler) reconcile(ctx context.Context, log logr.Logger, infra *extensionsv1alpha1.Infrastructure) (ctrl.Result, error) {
	config := &kindv1alpha1.InfrastructureConfig{}
	decoder := serializer.NewCodecFactory(r.Scheme).UniversalDecoder()
	if _, err := runtime.Decode(decoder, infra.Spec.ProviderConfig.Raw); err != nil {
		return ctrl.Result{}, fmt.Errorf("error decoding infrastructure config: %w", err)
	}
	_ = config

	base := infra.DeepCopy()
	infra.Status.ObservedGeneration = infra.Generation
	infra.Status.ProviderStatus = &runtime.RawExtension{
		Object: &kindv1alpha1.InfrastructureStatus{
			TypeMeta: metav1.TypeMeta{
				APIVersion: kindv1alpha1.GroupVersion.String(),
				Kind:       "InfrastructureStatus",
			},
		},
	}
	infra.Status.LastOperation = &gardenerv1beta1.LastOperation{
		Type:           "Apply",
		State:          gardenerv1beta1.LastOperationStateSucceeded,
		LastUpdateTime: metav1.Now(),
		Progress:       100,
	}
	if err := r.Status().Patch(ctx, infra, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not update status: %w", err)
	}
	return ctrl.Result{}, nil
}
