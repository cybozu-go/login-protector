package controller

import (
	"context"

	"github.com/cybozu-go/login-protector/internal/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// selectTargetStatefulSetPredicate returns a predicate that filters out a non target StatefulSet.
func selectTargetStatefulSetPredicate() predicate.Predicate {
	pred, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{
			Key:      common.LabelKeyLoginProtectorProtect,
			Operator: metav1.LabelSelectorOpIn,
			Values:   []string{common.ValueTrue},
		}},
	})
	if err != nil {
		// This should never happen.
		panic(err)
	}
	return pred
}

// selectTargetPodPredicate returns a predicate that filters out a non target Pod.
func selectTargetPodPredicate(ctx context.Context, cli client.Client) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(o client.Object) bool {
		owner := metav1.GetControllerOf(o)
		if owner == nil {
			return false
		}
		if owner.Kind != common.KindStatefulSet {
			return false
		}
		sts := &appsv1.StatefulSet{}
		if err := cli.Get(ctx, client.ObjectKey{Namespace: o.GetNamespace(), Name: owner.Name}, sts); err != nil {
			return false
		}
		if protect, ok := sts.Labels[common.LabelKeyLoginProtectorProtect]; ok {
			return protect == common.ValueTrue
		}
		return false
	})
}

// selectTargetPDBPredicate returns a predicate that filters out a non target PodDisruptionBudget.
func selectTargetPDBPredicate(ctx context.Context, cli client.Client) predicate.Predicate {
	return predicate.NewPredicateFuncs(func(o client.Object) bool {
		ownerPod := metav1.GetControllerOf(o)
		if ownerPod == nil {
			return false
		}
		if ownerPod.Kind != common.KindPod {
			return false
		}
		pod := &corev1.Pod{}
		if err := cli.Get(ctx, client.ObjectKey{Namespace: o.GetNamespace(), Name: ownerPod.Name}, pod); err != nil {
			return false
		}
		ownerSts := metav1.GetControllerOf(pod)
		if ownerSts == nil {
			return false
		}
		if ownerSts.Kind != common.KindStatefulSet {
			return false
		}
		sts := &appsv1.StatefulSet{}
		if err := cli.Get(ctx, client.ObjectKey{Namespace: o.GetNamespace(), Name: ownerSts.Name}, sts); err != nil {
			return false
		}
		if protect, ok := sts.Labels[common.LabelKeyLoginProtectorProtect]; ok {
			return protect == common.ValueTrue
		}
		return false
	})
}

func requestFromPDBFunc(cli client.Client) handler.TypedMapFunc[*policyv1.PodDisruptionBudget] {
	return func(ctx context.Context, pdb *policyv1.PodDisruptionBudget) []reconcile.Request {
		ownerPod := metav1.GetControllerOf(pdb)
		if ownerPod == nil {
			return nil
		}
		if ownerPod.Kind != common.KindPod {
			return nil
		}
		pod := &corev1.Pod{}
		if err := cli.Get(ctx, client.ObjectKey{Namespace: pdb.GetNamespace(), Name: ownerPod.Name}, pod); err != nil {
			return nil
		}
		ownerSts := metav1.GetControllerOf(pod)
		if ownerSts == nil {
			return nil
		}
		if ownerSts.Kind != common.KindStatefulSet {
			return nil
		}
		sts := &appsv1.StatefulSet{}
		if err := cli.Get(ctx, client.ObjectKey{Namespace: pdb.GetNamespace(), Name: ownerSts.Name}, sts); err != nil {
			return nil
		}
		if protect, ok := sts.Labels[common.LabelKeyLoginProtectorProtect]; ok {
			if protect != common.ValueTrue {
				return nil
			}
		}

		return []reconcile.Request{{NamespacedName: types.NamespacedName{
			Namespace: pdb.GetNamespace(),
			Name:      sts.Name,
		}}}
	}
}
