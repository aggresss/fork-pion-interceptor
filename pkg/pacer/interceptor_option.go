package pacer

import (
	"errors"
	"time"

	"github.com/pion/logging"
)

// PacerLog sets a logger for the interceptor.
func PacerLog(log logging.LeveledLogger) Option {
	return func(p *Interceptor) error {
		p.log = log
		return nil
	}
}

// PacerMaxSendDelay sets the max send delay of the interceptor.
func PacerMaxSendDelay(d time.Duration) Option {
	return func(p *Interceptor) error {
		if d < p.pacingInterval {
			return errors.New("maxSendDelay should larger than pacingInterval")
		}
		p.maxSendDelay = d
		return nil
	}
}
