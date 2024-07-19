package tty_exporter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/cybozu-go/login-protector/internal/common"
)

var errProcStat = errors.New("broken process stat")

// getTTYProcesses returns the number of controlling terminals observed.
// NOTE: This implementation is for Linux.
func getTTYProcesses() (*common.TTYProcesses, error) {
	res := &common.TTYProcesses{
		Total:     0,
		Processes: make([]common.Process, 0),
	}

	dirs, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}

	for _, d := range dirs {
		err := func() error {
			name := d.Name()
			for _, ch := range name {
				if ch < '0' || ch > '9' {
					// if the name contains non-digit characters, it is not a process directory.
					return nil
				}
			}
			statBytes, err := os.ReadFile(filepath.Join("/proc", name, "stat"))
			if err != nil {
				return err
			}

			stat := string(statBytes)
			fields := strings.Split(stat, " ")
			if len(fields) <= 6 {
				return errProcStat
			}

			// The 1st (0-origin) field is the filename of the executable enclosed in parentheses.
			tcomm := strings.Trim(strings.TrimLeft(fields[1], "("), ")")
			// The 6th (0-origin) field is controlling tty device number.
			// If it is "0", the process is not controlled.
			ttyNumber := fields[6]
			if ttyNumber != "0" {
				p := common.Process{
					PID:  name,
					Name: tcomm,
				}
				res.Total++
				res.Processes = append(res.Processes, p)
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
	}

	return res, nil
}
