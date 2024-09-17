package controller

import (
	"context"

	"github.com/cybozu-go/login-protector/internal/common"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const metricsNamespace = "login_protector"

var (
	pendingUpdatesDesc = prometheus.NewDesc(
		metricsNamespace+"_pod_pending_updates",
		"Describes whether the update of the Pod is pending.",
		[]string{"pod", "namespace"}, nil)

	protectingPodsDesc = prometheus.NewDesc(
		metricsNamespace+"_pod_protecting",
		"Describes whether the pod is being protected.",
		[]string{"pod", "namespace"}, nil)

	watcherErrorsCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: metricsNamespace,
			Name:      "watcher_errors_total",
			Help:      "Number of errors occurred in watchers.",
		},
		[]string{"watcher"},
	)
)

type metricsCollector struct {
	client.Client
	ctx    context.Context
	logger logr.Logger
}

func (c *metricsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- pendingUpdatesDesc
	ch <- protectingPodsDesc
	watcherErrorsCounter.Describe(ch)
}

func (c *metricsCollector) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()

	var stsList appsv1.StatefulSetList
	err := c.Client.List(ctx, &stsList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{common.LabelKeyLoginProtectorProtect: common.ValueTrue}),
	})
	if err != nil {
		c.logger.Error(err, "Unable to list StatefulSets for collecting metrics")
		return
	}

	for _, sts := range stsList.Items {
		pods := &corev1.PodList{}
		if err := c.Client.List(ctx, pods, client.InNamespace(sts.Namespace), client.MatchingLabels(sts.Spec.Selector.MatchLabels)); err != nil {
			c.logger.Error(err, "Unable to list Pods for collecting metrics")
			continue
		}
		for _, pod := range pods.Items {
			pending := 0.0
			rev, exists := pod.Labels[appsv1.ControllerRevisionHashLabelKey]
			if !exists || rev != sts.Status.UpdateRevision {
				pending = 1.0
			}
			ch <- prometheus.MustNewConstMetric(
				pendingUpdatesDesc,
				prometheus.GaugeValue,
				pending,
				pod.Name, pod.Namespace,
			)

			pdb := &policyv1.PodDisruptionBudget{}
			err = c.Client.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: pod.Name}, pdb)
			if err != nil && !k8serrors.IsNotFound(err) {
				c.logger.Error(err, "Unable to get PodDisruptionBudget for collecting metrics")
				continue
			}
			protecting := 0.0
			if err == nil {
				protecting = 1.0
			}
			ch <- prometheus.MustNewConstMetric(
				protectingPodsDesc,
				prometheus.GaugeValue,
				protecting,
				pod.Name, pod.Namespace,
			)
		}
	}
	watcherErrorsCounter.Collect(ch)
}

func SetupMetrics(ctx context.Context, c client.Client, logger logr.Logger) error {
	return metrics.Registry.Register(&metricsCollector{Client: c, ctx: ctx, logger: logger})
}
