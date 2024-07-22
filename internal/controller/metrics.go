package controller

import (
	"github.com/prometheus/client_golang/prometheus"
)

const metricsNamespace = "login_protector"

var metricsPDBCount = prometheus.NewGauge(
	prometheus.GaugeOpts{
		Namespace:   metricsNamespace,
		Name:        "pdb_count",
		Help:        "Number of crated PodDisruptionBudgets",
		ConstLabels: map[string]string{"controller": "pdb-controller"},
	},
)
var metricsReconcileTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Namespace:   metricsNamespace,
		Name:        "reconcile_total",
		Help:        "Number of reconciles",
		ConstLabels: map[string]string{"controller": "pdb-controller"},
	},
)
var metricsReconcileErrorsTotal = prometheus.NewCounter(
	prometheus.CounterOpts{
		Namespace:   metricsNamespace,
		Name:        "reconcile_errors_total",
		Help:        "Number of reconcile errors",
		ConstLabels: map[string]string{"controller": "pdb-controller"},
	},
)

var metricsPollingDurationSecondsHistogram = prometheus.NewHistogram(
	prometheus.HistogramOpts{
		Namespace:   metricsNamespace,
		Name:        "reconcile_time_seconds",
		Help:        "Duration of reconcile in seconds",
		ConstLabels: map[string]string{"controller": "pdb-controller"},
		Buckets:     []float64{0.001, 0.002, 0.004, 0.008, 0.012, 0.016, 0.024, 0.032, 0.064, 0.096, 0.128, 0.256, 0.512, 1.024},
	},
)

func init() {
	prometheus.MustRegister(
		metricsReconcileTotal,
		metricsReconcileErrorsTotal,
		metricsPollingDurationSecondsHistogram,
	)
}
