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
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type LocalSessionWatcher struct {
	client   client.Client
	logger   logr.Logger
	interval time.Duration
	channel  chan<- event.TypedGenericEvent[*corev1.Pod]
}

func NewLocalSessionWatcher(client client.Client, logger logr.Logger, interval time.Duration, ch chan<- event.TypedGenericEvent[*corev1.Pod]) *LocalSessionWatcher {
	return &LocalSessionWatcher{
		client:   client,
		logger:   logger,
		interval: interval,
		channel:  ch,
	}
}

func (w *LocalSessionWatcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	watcherErrorsCounter.WithLabelValues("local-session-watcher").Add(0)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			err := w.poll(ctx)
			if err != nil {
				w.logger.Error(err, "failed to poll")
				watcherErrorsCounter.WithLabelValues("local-session-watcher").Inc()
			}
		}
	}
}

func (w *LocalSessionWatcher) poll(ctx context.Context) error {
	var stsList appsv1.StatefulSetList
	err := w.client.List(ctx, &stsList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{common.LabelKeyLoginProtectorProtect: common.ValueTrue}),
	})
	if err != nil {
		w.logger.Error(err, "failed to list StatefulSets")
		return err
	}

	errList := make([]error, 0)
	// Get all pods that belong to the StatefulSets
	for _, sts := range stsList.Items {
		trackerName := common.DefaultTrackerName
		if name, ok := sts.Annotations[common.AnnotationKeyTrackerName]; ok {
			trackerName = name
		}
		trackerPort := common.DefaultTrackerPort
		if port, ok := sts.Annotations[common.AnnotationKeyTrackerPort]; ok {
			trackerPort = port
		}

		var podList corev1.PodList
		err = w.client.List(ctx, &podList, client.InNamespace(sts.Namespace), client.MatchingLabels(sts.Spec.Selector.MatchLabels))
		if err != nil {
			errList = append(errList, err)
			continue
		}

		for _, pod := range podList.Items {
			err = w.notify(ctx, pod, trackerName, trackerPort)
			if err != nil {
				errList = append(errList, err)
			}
		}
	}
	if len(errList) > 0 {
		return errors.Join(errList...)
	}
	return nil
}

// notify notifies pod-controller that the login status has changed
func (w *LocalSessionWatcher) notify(ctx context.Context, pod corev1.Pod, trackerName, trackerPort string) error {
	podIP := pod.Status.PodIP

	var container *corev1.Container
	for _, c := range pod.Spec.Containers {
		c := c
		if c.Name == trackerName {
			container = &c
			break
		}
	}
	if container == nil {
		err := fmt.Errorf("failed to find sidecar container (Name: %s)", trackerName)
		return err
	}

	resp, err := http.Get(fmt.Sprintf("http://%s:%s/status", podIP, trackerPort))
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

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	currentLoggedIn := pod.Annotations[common.AnnotationLoggedIn]

	if status.Total == 0 {
		pod.Annotations[common.AnnotationLoggedIn] = common.ValueFalse
	} else {
		pod.Annotations[common.AnnotationLoggedIn] = common.ValueTrue
	}

	if currentLoggedIn == pod.Annotations[common.AnnotationLoggedIn] {
		return nil
	}

	w.logger.Info("notify", "namespace", pod.Namespace, "pod", pod.Name, "current", currentLoggedIn, "new", pod.Annotations[common.AnnotationLoggedIn])

	err = w.client.Update(ctx, &pod)
	if err != nil {
		return err
	}

	ev := event.TypedGenericEvent[*corev1.Pod]{
		Object: pod.DeepCopy(),
	}
	w.channel <- ev

	return nil
}
