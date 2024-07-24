package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/cybozu-go/login-protector/internal/common"
	appsv1 "k8s.io/api/apps/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

type PDBReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=create;get;list;watch;delete

func (r *PDBReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	sts := &appsv1.StatefulSet{}
	if err := r.Client.Get(ctx, req.NamespacedName, sts); err != nil {
		logger.Error(err, "failed to get StatefulSet")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if sts.DeletionTimestamp != nil {
		logger.Info("the statefulset is being deleted")
		return ctrl.Result{}, nil
	}

	var podList corev1.PodList
	err := r.Client.List(ctx, &podList, client.InNamespace(sts.Namespace), client.MatchingLabels(sts.Spec.Selector.MatchLabels))
	if err != nil {
		return ctrl.Result{}, err
	}

	exporterName := "tty-exporter"
	if name, ok := sts.Annotations[common.AnnotationKeyExporterName]; ok {
		exporterName = name
	}
	exporterPort := "8080"
	if port, ok := sts.Annotations[common.AnnotationKeyExporterPort]; ok {
		exporterPort = port
	}
	noPDB := false
	if noPDBStr, ok := sts.Annotations[common.AnnotationKeyNoPDB]; ok {
		noPDB = noPDBStr == common.ValueTrue
	}

	errorList := make([]error, 0)
	for _, pod := range podList.Items {
		err = r.reconcilePDB(ctx, &pod, exporterName, exporterPort, noPDB)
		if err != nil {
			errorList = append(errorList, err)
		}
	}

	if len(errorList) > 0 {
		return ctrl.Result{}, fmt.Errorf("failed to reconcile PDB: %v", errorList)
	}

	return ctrl.Result{}, nil
}

func (r *PDBReconciler) reconcilePDB(ctx context.Context, pod *corev1.Pod, exporterName string, exporterPort string, noPDB bool) error {
	logger := log.FromContext(ctx)

	if pod.DeletionTimestamp != nil {
		logger.Info("the Pod is about to be deleted. skipping.")
		return nil
	}

	if val, ok := pod.Annotations[common.AnnotationKeyNoPDB]; ok {
		noPDB = noPDB || (val == common.ValueTrue)
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

	podIP := pod.Status.PodIP

	var container *corev1.Container
	for _, c := range pod.Spec.Containers {
		c := c
		if c.Name == exporterName {
			container = &c
			break
		}
	}
	if container == nil {
		err := fmt.Errorf("failed to find sidecar container (Name: %s)", exporterName)
		return err
	}

	resp, err := http.Get(fmt.Sprintf("http://%s:%s/status", podIP, exporterPort))
	if err != nil {
		return err
	}
	defer resp.Body.Close() // nolint:errcheck

	status := common.TTYStatus{}
	statusBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	err = json.Unmarshal(statusBytes, &status)
	if err != nil {
		return err
	}
	if status.Total < 0 {
		err = errors.New("broken status")
		return err
	}

	foundPdb := false
	pdb := &policyv1.PodDisruptionBudget{}
	err = r.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, pdb)
	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return err
		}
	} else {
		foundPdb = true
	}

	if status.Total == 0 {
		// no controlling terminals are observed. delete PDB.
		if !foundPdb {
			return nil
		}

		logger.Info("delete PDB", "pdb", pdb, "pod", pod.Name, "namespace", pod.Namespace)
		err = r.Client.Delete(ctx, pdb)
		if err != nil {
			return err
		}
	} else {
		// some controlling terminals are observed. create PDB.
		if foundPdb {
			return nil
		}

		zeroIntstr := intstr.FromInt32(0)
		pdb := &policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pod.Name,
				Namespace: pod.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "v1",
						Kind:       "Pod",
						Name:       pod.GetName(),
						UID:        pod.GetUID(),
					},
				},
			},
			Spec: policyv1.PodDisruptionBudgetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: pod.Labels,
				},
				MaxUnavailable: &zeroIntstr,
			},
		}
		logger.Info("crate a new PDB", "pdb", pdb, "pod", pod.Name, "namespace", pod.Namespace)
		err = r.Client.Create(ctx, pdb)
		if err != nil {
			return err
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PDBReconciler) SetupWithManager(mgr ctrl.Manager, ch chan event.TypedGenericEvent[*appsv1.StatefulSet]) error {
	targetStsPredicate, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{
			Key:      common.LabelKeyLoginProtectorProtect,
			Operator: metav1.LabelSelectorOpExists,
		}},
	})
	if err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}, builder.WithPredicates(targetStsPredicate)).
		WatchesRawSource(source.Channel(ch, &handler.TypedEnqueueRequestForObject[*appsv1.StatefulSet]{})).
		Complete(r)
}
