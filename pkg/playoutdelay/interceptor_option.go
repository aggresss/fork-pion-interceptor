package playoutdelay

import (
	"time"

	"github.com/pion/logging"
)

// PlayoutDelayLog sets a logger for the interceptor.
func PlayoutDelayLog(log logging.LeveledLogger) Option {
	return func(p *Interceptor) error {
		p.log = log
		return nil
	}
}

// PlayoutDelayMax sets the max playout delay of the interceptor.
func PlayoutDelayMax(d time.Duration) Option {
	return func(p *Interceptor) error {
		if d > playoutDelayLimitMax {
			d = playoutDelayLimitMax
		}
		p.maxDelay = uint16(d / time.Millisecond / 10)
		return nil
	}
}

// PlayoutDelayMin sets the min playout delay of the interceptor.
func PlayoutDelayMin(d time.Duration) Option {
	return func(p *Interceptor) error {
		if d > playoutDelayLimitMax {
			d = playoutDelayLimitMax
		}
		p.minDelay = uint16(d / time.Millisecond / 10)
		return nil
	}
}
