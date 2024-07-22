package common

// Process represents the process information
type Process struct {
	// PID represents the process ID
	PID string `json:"pid"`
	// Command represents the filename of the executable
	Command string `json:"command"`
}

// TTYStatus represents the TTY status information
type TTYStatus struct {
	// Total represents the total number of processes associated with TTY
	Total int `json:"total"`
	// Processes represents the list of processes associated with TTY
	Processes []Process `json:"processes"`
}
