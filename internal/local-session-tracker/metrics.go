package local_session_tracker

import (
	"math"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const metricsNamespace = "local_session_tracker"

func InitMetrics(logger *zap.Logger) {
	prometheus.MustRegister(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: metricsNamespace,
			Name:      "ttys",
			Help:      "Number of controlling terminals observed",
		},
		func() float64 {
			res, err := getTTYStatus()
			if err != nil {
				logger.Error("failed to count ttys", zap.Error(err))
				return math.NaN()
			}
			return float64(res.Total)
		},
	))
}
