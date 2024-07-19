package common

type Process struct {
	PID  string `json:"pid"`
	Name string `json:"name"`
}
type TTYProcesses struct {
	Total     int       `json:"total"`
	Processes []Process `json:"processes"`
}
