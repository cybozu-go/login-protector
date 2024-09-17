package controller

import (
	"context"
	"errors"
	"time"

	"github.com/cybozu-go/login-protector/internal/common"
	"github.com/go-logr/logr"
	teleport_client "github.com/gravitational/teleport/api/client"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

type TeleportSessionWatcher struct {
	kubernetesClient client.Client
	teleportClient   *teleport_client.Client
	logger           logr.Logger
	interval         time.Duration
	channel          chan<- event.TypedGenericEvent[*corev1.Pod]
}

func NewTeleportSessionWatcher(kubernetesClient client.Client, teleportClient *teleport_client.Client, logger logr.Logger, interval time.Duration, ch chan<- event.TypedGenericEvent[*corev1.Pod]) *TeleportSessionWatcher {
	return &TeleportSessionWatcher{
		kubernetesClient: kubernetesClient,
		teleportClient:   teleportClient,
		logger:           logger,
		interval:         interval,
		channel:          ch,
	}
}

func (w *TeleportSessionWatcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	watcherErrorsCounter.WithLabelValues("teleport-session-watcher").Add(0)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			err := w.poll(ctx)
			if err != nil {
				w.logger.Error(err, "failed to poll")
				watcherErrorsCounter.WithLabelValues("teleport-session-watcher").Inc()
			}
		}
	}
}

func (w *TeleportSessionWatcher) poll(ctx context.Context) error {
	var stsList appsv1.StatefulSetList
	err := w.kubernetesClient.List(ctx, &stsList, &client.ListOptions{
		LabelSelector: labels.SelectorFromSet(map[string]string{common.LabelKeyLoginProtectorProtect: common.ValueTrue}),
	})
	if err != nil {
		w.logger.Error(err, "failed to list StatefulSets")
		return err
	}

	sessions, err := w.getTeleportSessions(ctx)
	if err != nil {
		w.logger.Error(err, "failed to get teleport sessions")
		return err
	}

	errList := make([]error, 0)
	// Get all pods that belong to the StatefulSets
	for _, sts := range stsList.Items {
		var podList corev1.PodList
		err = w.kubernetesClient.List(ctx, &podList, client.InNamespace(sts.Namespace), client.MatchingLabels(sts.Spec.Selector.MatchLabels))
		if err != nil {
			errList = append(errList, err)
			continue
		}

		for _, pod := range podList.Items {
			err = w.notify(ctx, sessions, pod)
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

type TeleportSession struct {
	Kind     string `json:"kind"`
	HostName string `json:"hostname"`
	User     string `json:"user"`
	Login    string `json:"login"`
}

func (w *TeleportSessionWatcher) getTeleportSessions(ctx context.Context) (map[string][]TeleportSession, error) {
	trackers, err := w.teleportClient.GetActiveSessionTrackers(ctx)
	if err != nil {
		return nil, err
	}

	sessions := make(map[string][]TeleportSession)
	for _, tracker := range trackers {
		session := TeleportSession{
			Kind:     tracker.GetKind(),
			HostName: tracker.GetHostname(),
			User:     tracker.GetHostUser(),
			Login:    tracker.GetLogin(),
		}
		sessions[tracker.GetHostname()] = append(sessions[tracker.GetHostname()], session)
	}

	return sessions, nil
}

// notify notifies pod-controller that the login status has changed
func (w *TeleportSessionWatcher) notify(ctx context.Context, sessions map[string][]TeleportSession, pod corev1.Pod) error {
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	currentLoggedIn := pod.Annotations[common.AnnotationLoggedIn]

	session := sessions[pod.Name]
	if len(session) == 0 {
		pod.Annotations[common.AnnotationLoggedIn] = common.ValueFalse
	} else {
		pod.Annotations[common.AnnotationLoggedIn] = common.ValueTrue
	}

	if currentLoggedIn == pod.Annotations[common.AnnotationLoggedIn] {
		return nil
	}

	w.logger.Info("notify", "namespace", pod.Namespace, "pod", pod.Name, "current", currentLoggedIn, "new", pod.Annotations[common.AnnotationLoggedIn], "session", session)

	err := w.kubernetesClient.Update(ctx, &pod)
	if err != nil {
		return err
	}

	ev := event.TypedGenericEvent[*corev1.Pod]{
		Object: pod.DeepCopy(),
	}
	w.channel <- ev

	return nil
}
