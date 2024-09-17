package local_session_tracker

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/cybozu-go/login-protector/internal/common"
)

var errProcStat = errors.New("broken process stat")

// getTTYStatus returns the status of processes associated with TTY.
// NOTE: This implementation is for Linux.
func getTTYStatus() (*common.TTYStatus, error) {
	res := &common.TTYStatus{
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
			statFilePath := filepath.Join("/proc", name, "stat")
			statBytes, err := os.ReadFile(statFilePath)
			if err != nil {
				return err
			}

			stat := string(statBytes)
			fields := strings.Split(stat, " ")
			if len(fields) <= 6 {
				return errProcStat
			}

			// Get the owner of the process
			info, err := os.Stat(statFilePath)
			if err != nil {
				return err
			}
			owner := "unknown"
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				uid := strconv.Itoa(int(st.Uid))
				u, err := user.LookupId(uid)
				if err != nil {
					owner = uid
				} else {
					owner = u.Username
				}
			}

			// The 1st (0-origin) field is the filename of the executable enclosed in parentheses.
			tcomm := strings.Trim(strings.TrimLeft(fields[1], "("), ")")
			// The 6th (0-origin) field is controlling tty device number.
			// If it is "0", the process is not controlled.
			ttyNumber := fields[6]
			if ttyNumber != "0" {
				p := common.Process{
					PID:     name,
					Command: tcomm,
					User:    owner,
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
