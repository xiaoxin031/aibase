//go:build !linux && !darwin && !windows

package host

import (
	"context"
	"github.com/xiaoxin031/aibase/internal/cache"
	"time"
)

func (i *Inspector) platform() []probe { return nil }
func (i *Inspector) runtime() []probe {
	var out []probe
	for _, n := range []string{"listeners", "services", "containers", "resources"} {
		n := n
		out = append(out, probe{name: n, run: func(context.Context) (any, error) {
			return cache.New(nil, "platform", time.Minute, cache.Unsupported), nil
		}})
	}
	return out
}
