package red

import (
	"fmt"

	"github.com/pion/logging"
)

// RedLog sets a logger for the interceptor.
func RedLog(log logging.LeveledLogger) Option {
	return func(r *Interceptor) error {
		r.log = log
		return nil
	}
}

// RedMaxRedundantLevel sets the max redundant level of the interceptor.
func RedMaxRedundantLevel(l int) Option {
	return func(r *Interceptor) error {
		if l < redLevelMax {
			return fmt.Errorf("maxSendDelay should lower than %d", redLevelMax)
		}
		r.maxRedundantLevel = l
		return nil
	}
}
