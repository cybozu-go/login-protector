package tty_exporter

import (
	"encoding/json"
	"net/http"

	"github.com/cybozu-go/login-protector/internal/common"
	"go.uber.org/zap"
)

type StatusHandler struct {
	logger *zap.Logger
}

func NewStatusHandler(logger *zap.Logger) http.Handler {
	return &StatusHandler{
		logger: logger,
	}
}

func writeError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Header().Add("Content-Type", "text/plain")
	w.Write([]byte(err.Error())) //nolint:errcheck
}

func (h *StatusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	num, err := ttyCount()
	if err != nil {
		h.logger.Error("failed to count ttys", zap.Error(err))
		writeError(w, err)
		return
	}
	status := common.Status{
		TTYs: num,
	}
	out, err := json.Marshal(&status)
	if err != nil {
		h.logger.Error("failed to marshal", zap.Error(err))
		writeError(w, err)
		return
	}
	w.Header().Add("Content-Type", "application/json")
	w.Write(out) //nolint:errcheck
}
