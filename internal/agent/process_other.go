//go:build !linux

package agent

import (
	"context"
	"errors"

	"github.com/Sao-Ali/vetch/internal/workload"
)

// HostCapacity keeps process-health mode usable outside Linux; workers are unsupported.
func HostCapacity() (int64, int64, error) { return 0, 0, nil }

func RuntimeStarter(_, _ string) StartProcess {
	return func(context.Context, string, workload.WorkerConfig, int) (Process, error) {
		return nil, errors.New("limited inference workers require Linux cgroup v2")
	}
}
