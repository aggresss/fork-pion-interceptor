package playoutdelay

import (
	"errors"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtp"
)

const (
	PlayoutDelayURI = "http://www.webrtc.org/experiments/rtp-hdrext/playout-delay"
)

const (
	playoutDelayLimitMax = (1<<12 - 1) * 10 * time.Millisecond // 40950ms
	playoutDelayDefault  = 400 * time.Millisecond              // 400ms
)

var (
	errHeaderIsNil = errors.New("header is nil")
)

// Option can be used to set initial options on playout-delay interceptors
type Option func(*Interceptor) error

// InterceptorFactory is a factory for playout-delay interceptors
type InterceptorFactory struct {
	opts []Option
}

// NewInterceptor returns a new playout-delay interceptor factory
func NewInterceptor(opts ...Option) (*InterceptorFactory, error) {
	return &InterceptorFactory{
		opts: opts,
	}, nil
}

func (f *InterceptorFactory) NewInterceptor(_ string) (interceptor.Interceptor, error) {
	i := &Interceptor{
		log:      logging.NewDefaultLoggerFactory().NewLogger("playout-delay"),
		minDelay: uint16(playoutDelayDefault / time.Millisecond / 10),
		maxDelay: uint16(playoutDelayDefault / time.Millisecond / 10),
	}

	for _, opt := range f.opts {
		if err := opt(i); err != nil {
			return nil, err
		}
	}

	return i, nil
}

type Interceptor struct {
	interceptor.NoOp
	log logging.LeveledLogger

	minDelay uint16
	maxDelay uint16
}

// BindLocalStream returns a writer that adds a PlayoutDelayExtension
func (h *Interceptor) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {
	var hdrExtID uint8
	for _, e := range info.RTPHeaderExtensions {
		if e.URI == PlayoutDelayURI {
			hdrExtID = uint8(e.ID)
			break
		}
	}
	if hdrExtID == 0 { // Don't add header extension if ID is 0, because 0 is an invalid extension ID
		return writer
	}
	return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {

		pd, err := (&playoutDelayExtension{minDelay: h.minDelay, maxDelay: h.maxDelay}).Marshal()
		if err != nil {
			return 0, err
		}
		if header == nil {
			return 0, errHeaderIsNil
		}
		err = header.SetExtension(hdrExtID, pd)
		if err != nil {
			return 0, err
		}
		return writer.Write(header, payload, attributes)
	})
}
