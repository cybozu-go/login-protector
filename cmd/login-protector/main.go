package main

import (
	"crypto/tls"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/cybozu-go/login-protector/internal/controller"
	teleport_client "github.com/gravitational/teleport/api/client"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var sessionCheckInterval time.Duration
	var sessionWatcher string
	var teleportIdentityFile string
	var teleportNamespace string
	var teleportAddrs string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.DurationVar(&sessionCheckInterval, "session-check-interval", 5*time.Second, "interval to check session")
	flag.StringVar(&sessionWatcher, "session-watcher", "local", "session watcher to use (local or teleport)")
	flag.StringVar(&teleportIdentityFile, "teleport-identity-file", "", "The file path of Teleport Identity")
	flag.StringVar(&teleportNamespace, "teleport-namespace", "teleport", "The namespace of Teleport")
	flag.StringVar(&teleportAddrs, "teleport-addrs", "teleport-auth:3025", "The comma-separated list of Teleport addresses")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   metricsAddr,
			SecureServing: secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "login-protector.cybozu.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()
	setupLog.Info("creating statefulset controller")
	if err = (&controller.StatefulSetUpdater{
		Client:    mgr.GetClient(),
		ClientSet: kubernetes.NewForConfigOrDie(mgr.GetConfig()),
		Scheme:    mgr.GetScheme(),
	}).SetupWithManager(ctx, mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "StatefulSet")
		os.Exit(1)
	}

	setupLog.Info("creating session watcher", "type", sessionWatcher)
	ch := make(chan event.TypedGenericEvent[*corev1.Pod])

	var watcher manager.Runnable
	switch sessionWatcher {
	case "local":
		watcher = controller.NewLocalSessionWatcher(
			mgr.GetClient(),
			mgr.GetLogger().WithName("LocalSessionWatcher"),
			sessionCheckInterval,
			ch,
		)
	case "teleport":
		if teleportIdentityFile == "" {
			setupLog.Error(nil, "teleport-identity-file is required for teleport session watcher")
			os.Exit(1)
		}
		if teleportNamespace == "" {
			setupLog.Error(nil, "teleport-namespace is required for teleport session watcher")
			os.Exit(1)
		}
		if teleportAddrs == "" {
			setupLog.Error(nil, "teleport-addrs is required for teleport session watcher")
			os.Exit(1)
		}
		teleportClient, err := teleport_client.New(ctx, teleport_client.Config{
			Addrs: strings.Split(teleportAddrs, ","),
			Credentials: []teleport_client.Credentials{
				teleport_client.LoadIdentityFile(teleportIdentityFile),
			},
		})

		if err != nil {
			log.Fatalf("failed to create client: %v", err)
		}
		defer teleportClient.Close() // nolint: errcheck
		watcher = controller.NewTeleportSessionWatcher(
			mgr.GetClient(),
			teleportClient,
			mgr.GetLogger().WithName("TeleportSessionWatcher"),
			teleportNamespace,
			sessionCheckInterval,
			ch,
		)
	default:
		setupLog.Error(nil, "unknown session watcher", "type", sessionWatcher)
		os.Exit(1)
	}
	err = mgr.Add(watcher)
	if err != nil {
		setupLog.Error(err, "unable to add watcher", "type", sessionWatcher)
		os.Exit(1)
	}

	setupLog.Info("creating pod controller")
	if err = (&controller.PodReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(ctx, mgr, ch); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Pod")
		os.Exit(1)
	}

	setupLog.Info("creating metrics collector")
	if err = controller.SetupMetrics(ctx, mgr.GetClient(), mgr.GetLogger().WithName("metrics-collector")); err != nil {
		setupLog.Error(err, "unable to setup metrics")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
