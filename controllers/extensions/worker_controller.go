package extensions

import (
	"context"
	"fmt"
	"strings"
	"time"

	gardenerpredicate "github.com/gardener/gardener/extensions/pkg/predicate"
	"github.com/gardener/gardener/extensions/pkg/util"
	"github.com/gardener/gardener/pkg/utils/kubernetes/bootstraptoken"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/cluster-bootstrap/token/api"

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

func (r *WorkerReconciler) applyPool(ctx context.Context, log logr.Logger, worker *extensionsv1alpha1.Worker, pool *extensionsv1alpha1.WorkerPool, bootstrapToken string) error {
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
			"userdata": []byte(strings.Replace(string(pool.UserData), "<<BOOTSTRAP_TOKEN>>", bootstrapToken, -1)),
		},
	}
	if err := ctrl.SetControllerReference(worker, userDataSecret, r.Scheme); err != nil {
		return fmt.Errorf("could not set user data secret ownership: %w", err)
	}
	if err := r.Patch(ctx, userDataSecret, client.Apply, fieldOwner, client.ForceOwnership); err != nil {
		return fmt.Errorf("error applying user data secret: %w", err)
	}

	workerName := fmt.Sprintf("%s-%s", worker.Name, pool.Name)
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
					Labels: map[string]string{
						"worker": workerName,
						"networking.gardener.cloud/from-prometheus":      "allowed",
						"networking.gardener.cloud/to-dns":               "allowed",
						"networking.gardener.cloud/to-private-networks":  "allowed",
						"networking.gardener.cloud/to-public-networks":   "allowed",
						"networking.gardener.cloud/to-shoot-networks":    "allowed",
						"networking.gardener.cloud/to-shoot-apiserver":   "allowed",
						"networking.gardener.cloud/from-shoot-apiserver": "allowed",
					},
					Annotations: map[string]string{
						"checksum/userdata": SHA256(pool.UserData),
						"cluster-name":      worker.Name,
						"pool-name":         pool.Name,
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
							Env: []corev1.EnvVar{{
								Name: "NODE_NAME",
								ValueFrom: &corev1.EnvVarSource{
									FieldRef: &corev1.ObjectFieldSelector{
										FieldPath: "metadata.name",
									},
								},
							}},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "userdata",
									MountPath: "/etc/gardener-worker",
								},
								{
									Name:      "containerd",
									MountPath: "/var/lib/containerd",
								},
								{
									Name:      "modules",
									MountPath: "/lib/modules",
									ReadOnly:  true,
								},
								// {
								// 	Name:      "xtables-lock",
								// 	MountPath: "/run/xtables.lock",
								// 	ReadOnly:  false,
								// },
							},
							ReadinessProbe: &corev1.Probe{
								Handler: corev1.Handler{
									Exec: &corev1.ExecAction{
										Command: []string{"sh", "-c", "/opt/bin/kubectl --kubeconfig /var/lib/kubelet/kubeconfig-real get no $NODE_NAME"},
									},
								},
							},
							Lifecycle: &corev1.Lifecycle{
								PreStop: &corev1.Handler{
									Exec: &corev1.ExecAction{
										Command: []string{"sh", "-c", "/opt/bin/kubectl --kubeconfig /var/lib/kubelet/kubeconfig-real delete node $NODE_NAME || true"},
									},
								},
							},
							Ports: []corev1.ContainerPort{{
								ContainerPort: 30123,
								Name:          "vpn",
							}},
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
							Name: "containerd",
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
						// {
						// 	Name: "xtables-lock",
						// 	VolumeSource: corev1.VolumeSource{
						// 		HostPath: &corev1.HostPathVolumeSource{
						// 			Path: "/run/xtables.lock",
						// 		},
						// 	},
						// },
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

	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: worker.Namespace,
			Name:      "vpn-shoot",
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: map[string]string{"worker": workerName},
			Ports: []corev1.ServicePort{{
				Name:       "vpn",
				Port:       4314,
				TargetPort: intstr.FromInt(30123),
			}},
		},
	}

	if err := r.Patch(ctx, service, client.Apply, fieldOwner, client.ForceOwnership); err != nil {
		return err
	}

	networkPolicy := &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: networkingv1.SchemeGroupVersion.String(),
			Kind:       "NetworkPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: worker.Namespace,
			Name:      "allow-to-worker-nodes",
		},
		Spec: networkingv1.NetworkPolicySpec{
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{
					PodSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{{
							Key:      "worker",
							Operator: metav1.LabelSelectorOpExists,
						}},
					},
				}},
			}},
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{"networking.gardener.cloud/to-shoot-networks": "allowed"},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
		},
	}

	return r.Patch(ctx, networkPolicy, client.Apply, fieldOwner, client.ForceOwnership)
}

func (r *WorkerReconciler) reconcile(ctx context.Context, log logr.Logger, worker *extensionsv1alpha1.Worker) (ctrl.Result, error) {
	bootstrapToken, err := r.createBootstrapToken(ctx, worker)
	if err != nil {
		return ctrl.Result{}, err
	}

	for _, pool := range worker.Spec.Pools {
		pool := pool
		if err := r.applyPool(ctx, log, worker, &pool, bootstrapToken); err != nil {
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

func (r *WorkerReconciler) createBootstrapToken(ctx context.Context, worker *extensionsv1alpha1.Worker) (string, error) {
	_, shootClient, err := util.NewClientForShoot(ctx, r.Client, worker.Namespace, client.Options{Scheme: r.Scheme})
	if err != nil {
		return "", err
	}

	if err := r.applyNodeSelfDeleterRBAC(ctx, shootClient); err != nil {
		return "", err
	}

	tokenSecret, err := bootstraptoken.ComputeBootstrapToken(ctx, shootClient, "abcdef", "kind", 6*time.Hour)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s.%s", tokenSecret.Data[api.BootstrapTokenIDKey], tokenSecret.Data[api.BootstrapTokenSecretKey]), nil
}

func (r *WorkerReconciler) applyNodeSelfDeleterRBAC(ctx context.Context, shootClient client.Client) error {
	clusterRole := &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacv1.SchemeGroupVersion.String(),
			Kind:       "ClusterRole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "system:node-self-deleter",
		},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{""},
			Resources: []string{"nodes"},
			Verbs:     []string{"delete"},
		}},
	}

	if err := shootClient.Patch(ctx, clusterRole, client.Apply, fieldOwner, client.ForceOwnership); err != nil {
		return err
	}

	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacv1.SchemeGroupVersion.String(),
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "system:node-self-deleter",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     clusterRole.Name,
		},
		Subjects: []rbacv1.Subject{{
			APIGroup: rbacv1.GroupName,
			Kind:     "Group",
			Name:     "system:nodes",
		}},
	}

	return shootClient.Patch(ctx, clusterRoleBinding, client.Apply, fieldOwner, client.ForceOwnership)
}
