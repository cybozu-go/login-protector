package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/cybozu-go/login-protector/internal/common"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PDBController struct {
	client   client.Client
	logger   logr.Logger
	interval time.Duration
}

func NewPDBController(client client.Client, logger logr.Logger, interval time.Duration) *PDBController {
	return &PDBController{
		client:   client,
		logger:   logger,
		interval: interval,
	}
}

//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=create;get;list;watch;delete

func (w *PDBController) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.pollPods(ctx)
		}
	}
}

func (w *PDBController) pollPods(ctx context.Context) {
	w.logger.Info("polling start")
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		metricsPollingDurationSecondsHistogram.Observe(duration.Seconds())
		w.logger.Info("polling completed", "duration", duration.Round(time.Millisecond))
	}()

	var stsList appsv1.StatefulSetList
	err := w.client.List(ctx, &stsList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{common.LabelKeyLoginProtectorTarget: "true"}),
	})
	if err != nil {
		w.logger.Error(err, "failed to list StatefulSets")
		return
	}

	targetPods := make([]corev1.Pod, 0)

	// Get all pods that belong to the StatefulSets
	for _, sts := range stsList.Items {
		var podList corev1.PodList
		err = w.client.List(ctx, &podList, client.InNamespace(sts.Namespace), client.MatchingLabels(sts.Spec.Selector.MatchLabels))
		if err != nil {
			w.logger.Error(err, "failed to list Pods")
			return
		}
		targetPods = append(targetPods, podList.Items...)
	}

	for _, pod := range targetPods {
		w.reconcilePDB(ctx, &pod)
	}
}

func (w *PDBController) reconcilePDB(ctx context.Context, pod *corev1.Pod) {
	if pod.DeletionTimestamp != nil {
		w.logger.Info("the Pod is about to be deleted. skipping.")
		return
	}

	podIP := pod.Status.PodIP

	var container *corev1.Container
	for _, c := range pod.Spec.Containers {
		c := c
		if c.Name == "tty-exporter" {
			container = &c
			break
		}
	}
	if container == nil {
		w.logger.Error(errors.New("failed to find sidecar container"), "failed to find sidecar container")
		return
	}
	if len(container.Ports) < 1 {
		w.logger.Error(errors.New("failed to get sidecar container port"), "failed to get sidecar container port")
		return
	}

	resp, err := http.Get(fmt.Sprintf("http://%s:%d/status", podIP, container.Ports[0].ContainerPort))
	if err != nil {
		w.logger.Error(err, "failed to get status")
		return
	}
	defer resp.Body.Close() // nolint:errcheck

	procs := common.TTYProcesses{}
	procsBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		w.logger.Error(err, "failed to get status")
		return
	}
	err = json.Unmarshal(procsBytes, &procs)
	if err != nil {
		w.logger.Error(err, "failed to unmarshal status")
		return
	}
	if procs.Total < 0 {
		w.logger.Error(errors.New("broken status"), "broken status")
		return
	}

	foundPdb := false
	pdb := &policyv1.PodDisruptionBudget{}
	err = w.client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, pdb)
	if err != nil {
		var k8serr *k8serrors.StatusError
		if !errors.As(err, &k8serr) || k8serr.Status().Reason != metav1.StatusReasonNotFound {
			w.logger.Error(err, "failed to check PDB")
			return
		}
	} else {
		foundPdb = true
	}

	if procs.Total == 0 {
		// no controlling terminals are observed. delete PDB.
		if !foundPdb {
			w.logger.Info("PDB does not exist")
			return
		}

		err = w.client.Delete(ctx, pdb)
		if err != nil {
			w.logger.Error(err, "failed to delete PDB")
		} else {
			w.logger.Info("deleted PDB")
		}
	} else {
		// some controlling terminals are observed. create PDB.
		if foundPdb {
			w.logger.Info("PDB already exists")
			return
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
		err := w.client.Create(ctx, pdb)
		if err != nil {
			w.logger.Error(err, "failed to create PDB")
		} else {
			w.logger.Info("created PDB")
		}
	}
}
