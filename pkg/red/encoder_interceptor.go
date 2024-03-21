package red

import (
	"sync"

	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtp"
)

const (
	redLevelMax     = 9
	redLevelDefault = 2
)

// Option can be used to set initial options on pacer interceptors
type Option func(*Interceptor) error

// InterceptorFactory is a factory for pacer interceptors
type InterceptorFactory struct {
	opts []Option
}

// NewInterceptor returns a new pacer interceptor factory
func NewInterceptor(opts ...Option) (*InterceptorFactory, error) {
	return &InterceptorFactory{
		opts: opts,
	}, nil
}

type Interceptor struct {
	interceptor.NoOp
	log logging.LeveledLogger

	ssrcToRedEncoder map[uint32]*RedEncoder
	mutex            sync.RWMutex

	maxRedundantLevel int
}

func (f *InterceptorFactory) NewInterceptor(_ string) (interceptor.Interceptor, error) {
	i := &Interceptor{
		log:               logging.NewDefaultLoggerFactory().NewLogger("red"),
		ssrcToRedEncoder:  map[uint32]*RedEncoder{},
		maxRedundantLevel: redLevelDefault,
	}

	for _, opt := range f.opts {
		if err := opt(i); err != nil {
			return nil, err
		}
	}

	return i, nil
}

// BindLocalStream lets you modify any outgoing RTP packets. It is called once
// for per LocalStream. The returned method will be called once per rtp packet.
func (p *Interceptor) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {
	if info == nil || writer == nil {
		return writer
	}

	if info.MimeType == "audio/opus" && info.Attributes.Get("red_pt") != nil {
		p.mutex.Lock()
		defer p.mutex.Unlock()

		p.ssrcToRedEncoder[info.SSRC] = NewRedEncoder(info.Attributes.Get("red_pt").(uint8))

		return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
			encoder := p.getRedEncoder(header.SSRC)
			if encoder != nil {
				if redHeader, redPayload, err := encoder.EncodeRed(header, payload, p.maxRedundantLevel); err == nil {
					return writer.Write(redHeader, redPayload, attributes)
				}
			}
			return writer.Write(header, payload, attributes)
		})
	}

	return writer
}

// UnbindLocalStream is called when the Stream is removed. It can be used to clean up any data related to that track.
func (p *Interceptor) UnbindLocalStream(info *interceptor.StreamInfo) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	delete(p.ssrcToRedEncoder, info.SSRC)
}

// Close closes the interceptor and the associated bandwidth estimator.
func (p *Interceptor) Close() error {
	return nil
}

func (p *Interceptor) getRedEncoder(ssrc uint32) *RedEncoder {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return p.ssrcToRedEncoder[ssrc]
}
