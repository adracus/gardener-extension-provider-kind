package extensions

import (
	"context"
	"fmt"

	gardenerpredicate "github.com/gardener/gardener/extensions/pkg/predicate"

	gardenerv1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	autoscalingv1 "k8s.io/api/autoscaling/v1"

	"k8s.io/utils/pointer"

	corev1 "k8s.io/api/core/v1"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	"k8s.io/apimachinery/pkg/runtime"

	kindv1alpha1 "github.com/gardener/gardener-extension-provider-kind/apis/kind/v1alpha1"

	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type WorkerReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	NodeImage string
}

// +kubebuilder:rbac:groups=extensions.gardener.cloud,resources=workers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=extensions.gardener.cloud,resources=workers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;update;patch

func (r *WorkerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	worker := &extensionsv1alpha1.Worker{}
	if err := r.Get(ctx, req.NamespacedName, worker); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !worker.DeletionTimestamp.IsZero() {
		return r.delete(ctx, log, worker)
	}

	if requeue, err := removeOperationAnnotation(ctx, r, worker); err != nil || requeue {
		return ctrl.Result{Requeue: requeue}, err
	}

	return r.reconcile(ctx, log, worker)
}

func (r *WorkerReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&extensionsv1alpha1.Worker{},
			builder.WithPredicates(
				predicate.NewPredicateFuncs(func(object client.Object) bool {
					worker := object.(*extensionsv1alpha1.Worker)
					return worker.Spec.Type == kindv1alpha1.ExtensionType
				}),
				predicate.Or(
					gardenerpredicate.HasOperationAnnotation(),
					predicate.GenerationChangedPredicate{},
				),
			),
		).
		Owns(&corev1.Secret{}).
		Owns(&autoscalingv1.HorizontalPodAutoscaler{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

func (r *WorkerReconciler) delete(ctx context.Context, log logr.Logger, worker *extensionsv1alpha1.Worker) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

func (r *WorkerReconciler) applyPool(ctx context.Context, log logr.Logger, worker *extensionsv1alpha1.Worker, pool *extensionsv1alpha1.WorkerPool) error {
	userDataSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: worker.Namespace,
			Name:      fmt.Sprintf("%s-%s-userdata", worker.Name, pool.Name),
		},
		Data: map[string][]byte{
			"userdata": pool.UserData,
		},
	}
	if err := ctrl.SetControllerReference(worker, userDataSecret, r.Scheme); err != nil {
		return fmt.Errorf("could not set user data secret ownership: %w", err)
	}
	if err := r.Patch(ctx, userDataSecret, client.Apply, fieldOwner, client.ForceOwnership); err != nil {
		return fmt.Errorf("error applying user data secret: %w", err)
	}

	workerName := fmt.Sprintf("%s-%s", worker.Name, pool.Name)
	fileOrCreate := corev1.HostPathFileOrCreate
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: worker.Namespace,
			Name:      workerName,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"worker": workerName},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"worker": workerName},
					Annotations: map[string]string{
						"checksum/userdata": SHA256(pool.UserData),
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "node",
							Image:           r.NodeImage,
							ImagePullPolicy: corev1.PullIfNotPresent,
							SecurityContext: &corev1.SecurityContext{
								Privileged: pointer.Bool(true),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "userdata",
									MountPath: "/etc/gardener-worker",
								},
								{
									Name:      "var",
									MountPath: "/var",
								},
								{
									Name:      "kind",
									MountPath: "/kind",
								},
								{
									Name:      "modules",
									MountPath: "/lib/modules",
									ReadOnly:  true,
								},
								{
									Name:      "xtables-lock",
									MountPath: "/run/xtables.lock",
									ReadOnly:  true,
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "userdata",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName:  userDataSecret.Name,
									DefaultMode: pointer.Int32(0777),
								},
							},
						},
						{
							Name: "var",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "kind",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
						{
							Name: "modules",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/lib/modules",
								},
							},
						},
						{
							Name: "xtables-lock",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/run/xtables.lock",
									Type: &fileOrCreate,
								},
							},
						},
					},
				},
			},
		},
	}
	if err := ctrl.SetControllerReference(worker, deployment, r.Scheme); err != nil {
		return fmt.Errorf("could not set deployment ownership: %w", err)
	}
	if err := r.Patch(ctx, deployment, client.Apply, fieldOwner, client.ForceOwnership); err != nil {
		return fmt.Errorf("error applying deployment: %w", err)
	}

	hpa := &autoscalingv1.HorizontalPodAutoscaler{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HorizontalPodAutoscaler",
			APIVersion: "autoscaling/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: worker.Namespace,
			Name:      workerName,
		},
		Spec: autoscalingv1.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv1.CrossVersionObjectReference{
				Kind:       deployment.Kind,
				Name:       deployment.Name,
				APIVersion: deployment.APIVersion,
			},
			MinReplicas: pointer.Int32(pool.Minimum),
			MaxReplicas: pool.Maximum,
		},
	}
	if err := ctrl.SetControllerReference(worker, hpa, r.Scheme); err != nil {
		return fmt.Errorf("could not set horizontal pod autoscaler ownership: %w", err)
	}
	if err := r.Patch(ctx, hpa, client.Apply, fieldOwner, client.ForceOwnership); err != nil {
		return fmt.Errorf("error applying horizontal pod autoscaler: %w", err)
	}

	return nil
}

func (r *WorkerReconciler) reconcile(ctx context.Context, log logr.Logger, worker *extensionsv1alpha1.Worker) (ctrl.Result, error) {
	for _, pool := range worker.Spec.Pools {
		pool := pool
		if err := r.applyPool(ctx, log, worker, &pool); err != nil {
			return ctrl.Result{}, fmt.Errorf("error applying pool: %w", err)
		}
	}

	base := worker.DeepCopy()
	worker.Status.ObservedGeneration = worker.Generation
	worker.Status.LastOperation = &gardenerv1beta1.LastOperation{
		Type:           "Apply",
		State:          gardenerv1beta1.LastOperationStateSucceeded,
		LastUpdateTime: metav1.Now(),
		Progress:       100,
	}
	if err := r.Status().Patch(ctx, worker, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, fmt.Errorf("could not update status: %w", err)
	}

	return ctrl.Result{}, nil
}
