package controller

import (
	"context"

	"github.com/cybozu-go/login-protector/internal/common"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type PodReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;update;watch
//+kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=create;get;list;watch;delete

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	pod := &corev1.Pod{}
	if err := r.Client.Get(ctx, req.NamespacedName, pod); err != nil {
		logger.Error(err, "failed to get Pod")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if pod.DeletionTimestamp != nil {
		logger.Info("the pod is being deleted")
		return ctrl.Result{}, nil
	}

	err := r.reconcilePDB(ctx, pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PodReconciler) reconcilePDB(ctx context.Context, pod *corev1.Pod) error {
	logger := log.FromContext(ctx)

	noPDB := false
	if val, ok := pod.Annotations[common.AnnotationKeyNoPDB]; ok {
		noPDB = val == common.ValueTrue
	}
	if noPDB {
		pdb := &policyv1.PodDisruptionBudget{}
		err := r.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, pdb)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return err
			}
		} else {
			logger.Info("delete PDB, because pod has no PDB annotation", "pdb", pdb, "pod", pod.Name, "namespace", pod.Namespace)
			err = r.Client.Delete(ctx, pdb)
			if err != nil {
				return err
			}
		}
		return nil
	}

	foundPdb := false
	pdb := &policyv1.PodDisruptionBudget{}
	err := r.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, pdb)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
	} else {
		foundPdb = true
	}

	loggedIn := pod.Annotations[common.AnnotationLoggedIn] == common.ValueTrue
	logger.Info("reconcile PDB", "pod", pod.Name, "namespace", pod.Namespace, "loggedIn", loggedIn, "foundPdb", foundPdb)

	if !loggedIn {
		// no controlling terminals are observed. delete PDB.
		if !foundPdb {
			return nil
		}

		logger.Info("delete PDB", "pdb", pdb, "pod", pod.Name, "namespace", pod.Namespace)
		err = r.Client.Delete(ctx, pdb)
		if err != nil {
			return err
		}
		return nil
	}

	// some controlling terminals are observed. create PDB.
	if foundPdb {
		return nil
	}

	zeroIntstr := intstr.FromInt32(0)
	pdb = &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: pod.Labels,
			},
			MaxUnavailable: &zeroIntstr,
		},
	}
	err = ctrl.SetControllerReference(pod, pdb, r.Scheme)
	if err != nil {
		logger.Error(err, "failed to set controller reference")
		return err
	}
	logger.Info("crate a new PDB", "pdb", pdb, "pod", pod.Name, "namespace", pod.Namespace)
	err = r.Client.Create(ctx, pdb)
	if err != nil {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, ch chan event.TypedGenericEvent[*corev1.Pod]) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}, builder.WithPredicates(selectTargetPodPredicate(ctx, mgr.GetClient()))).
		Owns(&policyv1.PodDisruptionBudget{}, builder.WithPredicates(selectTargetPDBPredicate(ctx, mgr.GetClient()))).
		WatchesRawSource(source.Channel(ch, &handler.TypedEnqueueRequestForObject[*corev1.Pod]{})).
		Complete(r)
}
