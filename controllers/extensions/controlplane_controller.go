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
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ControlPlaneReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=extensions.gardener.cloud,resources=controlplanes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=extensions.gardener.cloud,resources=controlplanes/status,verbs=get;update;patch

func (r *ControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	ctrlPlane := &extensionsv1alpha1.ControlPlane{}
	if err := r.Get(ctx, req.NamespacedName, ctrlPlane); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !ctrlPlane.DeletionTimestamp.IsZero() {
		return r.delete(ctx, log, ctrlPlane)
	}
	return r.reconcile(ctx, log, ctrlPlane)
}

func (r *ControlPlaneReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&extensionsv1alpha1.ControlPlane{},
			builder.WithPredicates(
				predicate.NewPredicateFuncs(func(object client.Object) bool {
					ctrlPlane := object.(*extensionsv1alpha1.ControlPlane)
					return ctrlPlane.Spec.Type == kindv1alpha1.ExtensionType
				}),
				predicate.Or(
					predicate.GenerationChangedPredicate{},
					gardenerpredicate.HasOperationAnnotation(),
				),
			),
		).
		Complete(r)
}

func (r *ControlPlaneReconciler) delete(ctx context.Context, log logr.Logger, ctrlPlane *extensionsv1alpha1.ControlPlane) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *ControlPlaneReconciler) reconcile(ctx context.Context, log logr.Logger, ctrlPlane *extensionsv1alpha1.ControlPlane) (ctrl.Result, error) {
	base := ctrlPlane.DeepCopy()
	ctrlPlane.Status.ObservedGeneration = ctrlPlane.Generation
	ctrlPlane.Status.LastOperation = &gardenerv1beta1.LastOperation{
		Type:           "Apply",
		State:          gardenerv1beta1.LastOperationStateSucceeded,
		LastUpdateTime: metav1.Now(),
		Progress:       100,
	}
	if err := r.Status().Patch(ctx, ctrlPlane, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not update status: %w", err)
	}
	return ctrl.Result{}, nil
}
