// Package processes 提供本机进程、端口及环境变量查询。
package processes

import "context"

type Process struct {
	PID, PPID                          int
	Name, Path, Command, User, Started string
	CPU                                float64
	Memory                             uint64
	Ports                              []Port
}

type Port struct {
	Protocol, Local, Remote, State string
	Number                         int
}

type Snapshot struct {
	Processes []Process
	Warning   string
}

type Details struct {
	Environment       map[string]string
	ParentEnvironment map[string]string
	Warning           string
}

type Provider interface {
	Snapshot(context.Context) (Snapshot, error)
	Details(context.Context, Process) (Details, error)
	Terminate(context.Context, Process, bool) error
}
