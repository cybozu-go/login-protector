package controller

import (
	"context"

	"github.com/cybozu-go/login-protector/internal/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// StatefulSetUpdater reconciles a StatefulSet object
type StatefulSetUpdater struct {
	Client    client.Client
	ClientSet kubernetes.Interface
	Scheme    *runtime.Scheme
}

//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=statefulsets/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods/eviction,verbs=create

func (u *StatefulSetUpdater) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	sts := &appsv1.StatefulSet{}
	if err := u.Client.Get(ctx, req.NamespacedName, sts); err != nil {
		logger.Error(err, "failed to get StatefulSet")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if sts.DeletionTimestamp != nil {
		logger.Info("StatefulSet is being deleted")
		return ctrl.Result{}, nil
	}

	if sts.Spec.UpdateStrategy.Type != appsv1.OnDeleteStatefulSetStrategyType {
		logger.Info("StatefulSet is not using `OnDelete` update strategy")
		return ctrl.Result{}, nil
	}

	target := sts.Labels[common.LabelKeyLoginProtectorProtect]
	if target != common.ValueTrue {
		logger.Info("StatefulSet is not a target")
		return ctrl.Result{}, nil
	}

	requeue, err := u.evictPod(ctx, sts)
	if err != nil {
		return ctrl.Result{}, err
	}
	if requeue {
		return ctrl.Result{Requeue: true}, nil
	}

	// When all pods are up-to-date, update the currentRevision of the StatefulSet
	// This behavior may be a bug in Kubernetes. See the following issue for details.
	// https://github.com/kubernetes/kubernetes/issues/106055
	if sts.Status.UpdateRevision != sts.Status.CurrentRevision {
		logger.Info("all pods are up-to-date, but currentRevision does not match updateRevision, updating status")
		sts.Status.CurrentRevision = sts.Status.UpdateRevision
		if err := u.Client.Status().Update(ctx, sts); err != nil {
			logger.Error(err, "failed to update StatefulSet status")
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (u *StatefulSetUpdater) evictPod(ctx context.Context, sts *appsv1.StatefulSet) (bool, error) {
	logger := log.FromContext(ctx)

	// Get pods that belong to the StatefulSet
	pods := &corev1.PodList{}
	if err := u.Client.List(ctx, pods, client.InNamespace(sts.Namespace), client.MatchingLabels(sts.Spec.Selector.MatchLabels)); err != nil {
		logger.Error(err, "failed to list pods")
		return false, err
	}

	// get pods whose specs have not been updated
	var outdatedPods []corev1.Pod
	for _, pod := range pods.Items {
		rev, exists := pod.Labels[appsv1.ControllerRevisionHashLabelKey]
		if !exists || rev != sts.Status.UpdateRevision {
			logger.Info("pod is outdated", "pod", pod.Name, "namespace", pod.Namespace)
			outdatedPods = append(outdatedPods, pod)
		}
	}

	if len(outdatedPods) == 0 {
		// All pods are up-to-date
		return false, nil
	}

	// Evict one of the outdated pods
	var pod *corev1.Pod
	for _, p := range outdatedPods {
		p := p
		if p.Status.Phase != corev1.PodRunning {
			logger.Info("not running pod found", "pod", p.Name, "namespace", p.Namespace)
			pod = &p
			break
		}
	}
	if pod == nil {
		pod = &outdatedPods[0]
	}

	logger.Info("evict outdated pod", "pod", pod.Name, "namespace", pod.Namespace)
	eviction := policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
	}
	if err := u.ClientSet.CoreV1().Pods(pod.Namespace).EvictV1(ctx, &eviction); err != nil {
		logger.Error(err, "failed to evict pod", "pod", pod.Name, "namespace", pod.Namespace)
		if apierrors.IsTooManyRequests(err) || apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	logger.Info("Successfully evict pod", "pod", pod.Name, "namespace", pod.Namespace)

	return true, nil
}

// SetupWithManager sets up the controller with the Manager.
func (u *StatefulSetUpdater) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}, builder.WithPredicates(selectTargetStatefulSetPredicate())).
		Owns(&corev1.Pod{}, builder.WithPredicates(selectTargetPodPredicate(ctx, mgr.GetClient()))).
		WatchesRawSource(source.Kind(mgr.GetCache(), &policyv1.PodDisruptionBudget{}, handler.TypedEnqueueRequestsFromMapFunc(requestFromPDBFunc(mgr.GetClient())))).
		Complete(u)
}
