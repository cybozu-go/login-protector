package main

import (
	"context"
	"fmt"
	"github.com/cybozu-go/login-protector/internal/common"
	tty_exporter "github.com/cybozu-go/login-protector/internal/tty-exporter"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
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
	os.Exit(run())
}

func run() int {
	logger := newZapLogger()
	defer logger.Sync()
	logger.Info("starting ttypdb-sidecar...")

	tty_exporter.InitMetrics(logger)

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
	mux.Handle("/status", tty_exporter.NewStatusHandler(logger))
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
			server.Shutdown(context.Background())
		}()
		err := server.ListenAndServe()
		if err != http.ErrServerClosed {
			logger.Error("failed to start HTTP server", zap.Error(err))
		}
	}()

	wg.Wait()
	logger.Info("termination completed")
	return 0
}

func handleReadyz(http.ResponseWriter, *http.Request) {
	// Nothing to do for now
}
