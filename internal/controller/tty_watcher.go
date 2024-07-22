package controller

import (
	"context"
	"github.com/cybozu-go/login-protector/internal/common"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"time"
)

type TTYWatcher struct {
	client   client.Client
	logger   logr.Logger
	interval time.Duration
	channel  chan<- event.TypedGenericEvent[*appsv1.StatefulSet]
}

func NewTTYWatcher(client client.Client, logger logr.Logger, interval time.Duration, ch chan<- event.TypedGenericEvent[*appsv1.StatefulSet]) *TTYWatcher {
	return &TTYWatcher{
		client:   client,
		logger:   logger,
		interval: interval,
		channel:  ch,
	}
}

func (w *TTYWatcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			w.pollStatefulSets(ctx)
		}
	}
}

func (w *TTYWatcher) pollStatefulSets(ctx context.Context) {
	startTime := time.Now()
	defer func() {
		duration := time.Since(startTime)
		metricsPollingDurationSecondsHistogram.Observe(duration.Seconds())
	}()

	var stsList appsv1.StatefulSetList
	err := w.client.List(ctx, &stsList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{common.LabelKeyLoginProtectorProtect: "true"}),
	})
	if err != nil {
		w.logger.Error(err, "failed to list StatefulSets")
		return
	}

	// Get all pods that belong to the StatefulSets
	for _, sts := range stsList.Items {
		w.channel <- event.TypedGenericEvent[*appsv1.StatefulSet]{
			Object: sts.DeepCopy(),
		}
	}
}
