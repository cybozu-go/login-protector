package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/cybozu-go/login-protector/internal/common"
	local_session_tracker "github.com/cybozu-go/login-protector/internal/local-session-tracker"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

const httpServerPort = 8080

func newZapLogger() *zap.Logger {
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	return logger
}

func main() {
	logger := newZapLogger()
	defer logger.Sync() //nolint:errcheck
	logger.Info("starting local-session-tracker...")

	local_session_tracker.InitMetrics(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wg := sync.WaitGroup{}

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGTERM, syscall.SIGINT)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		select {
		case signal := <-signalCh:
			logger.Info("caught signal", zap.String("signal", signal.String()))
		case <-ctx.Done():
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", handleReadyz)
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/status", local_session_tracker.NewStatusHandler(logger))
	server := http.Server{
		Addr:    fmt.Sprintf(":%d", httpServerPort),
		Handler: common.NewProxyHTTPHandler(mux, logger),
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel()
		go func() {
			<-ctx.Done()
			server.Shutdown(context.Background()) //nolint:errcheck
		}()
		err := server.ListenAndServe()
		if err != http.ErrServerClosed {
			logger.Error("failed to start HTTP server", zap.Error(err))
		}
	}()

	wg.Wait()
	logger.Info("termination completed")
}

func handleReadyz(http.ResponseWriter, *http.Request) {
	// Nothing to do for now
}
