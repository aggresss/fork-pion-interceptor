// Package pacer implements an interceptor for send pacing.
package pacer

import (
	"container/list"
	"encoding/csv"
	"errors"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtp"
)

const (
	RFC3339Milli = "2006-01-02T15:04:05.000Z07:00"
)

var (
	errPacerPoolCastFailed = errors.New("failed to access pacer pool, cast failed")
	errPacerStreamNotFound = errors.New("failed to get stream")
)

const (
	priorityLevelMax = 3
)

type item struct {
	header     *rtp.Header
	payload    *[]byte
	size       int
	attributes interceptor.Attributes
	timestamp  time.Time
}

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

func (f *InterceptorFactory) NewInterceptor(_ string) (interceptor.Interceptor, error) {
	i := &Interceptor{
		log:            logging.NewDefaultLoggerFactory().NewLogger("pacer"),
		pacingInterval: time.Millisecond * 5,
		maxSendDelay:   time.Millisecond * 40,
		done:           make(chan struct{}),
		ssrcToWriter:   map[uint32]interceptor.RTPWriter{},
		ssrcToPriority: map[uint32]int{},
	}

	i.pool = &sync.Pool{
		New: func() interface{} {
			b := make([]byte, 1460)
			return &b
		},
	}

	for l := 0; l <= priorityLevelMax; l++ {
		i.priorityQueue = append(i.priorityQueue, list.New())
	}

	for _, opt := range f.opts {
		if err := opt(i); err != nil {
			return nil, err
		}
	}

	go i.run()

	return i, nil
}

type Interceptor struct {
	interceptor.NoOp
	log logging.LeveledLogger

	pacingInterval time.Duration
	maxSendDelay   time.Duration

	done chan struct{}
	pool *sync.Pool

	priorityQueue []*list.List
	pqSize        int
	pqLock        sync.RWMutex

	ssrcToWriter   map[uint32]interceptor.RTPWriter
	ssrcToPriority map[uint32]int
	ssrcLock       sync.RWMutex

	latestBudget int
}

// BindLocalStream lets you modify any outgoing RTP packets. It is called once
// for per LocalStream. The returned method will be called once per rtp packet.
func (p *Interceptor) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {
	if info == nil || writer == nil {
		return writer
	}
	p.ssrcLock.Lock()
	defer p.ssrcLock.Unlock()
	p.ssrcToWriter[info.SSRC] = writer
	p.ssrcToPriority[info.SSRC] = getPriorityLevelFromStreamInfo(info)

	return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
		prio, err := p.getStreamPriority(header.SSRC)
		if err != nil {
			return 0, err
		}

		buf, ok := p.pool.Get().(*[]byte)
		if !ok {
			return 0, errPacerPoolCastFailed
		}

		copy(*buf, payload)
		hdr := header.Clone()
		p.pushItem(&item{
			header:     &hdr,
			payload:    buf,
			size:       len(payload),
			attributes: attributes,
			timestamp:  time.Now(),
		}, prio)

		return header.MarshalSize() + len(payload), nil
	})
}

// UnbindLocalStream is called when the Stream is removed. It can be used to clean up any data related to that track.
func (p *Interceptor) UnbindLocalStream(info *interceptor.StreamInfo) {
	p.ssrcLock.Lock()
	defer p.ssrcLock.Unlock()
	delete(p.ssrcToWriter, info.SSRC)
	delete(p.ssrcToPriority, info.SSRC)
}

// Close closes the interceptor and the associated bandwidth estimator.
func (p *Interceptor) Close() error {
	close(p.done)
	return nil
}

func (p *Interceptor) run() {
	f, err := os.Create(strconv.FormatInt(time.Now().UnixMilli(), 10) + ".csv")
	if err != nil {
		log.Fatalln("failed to open file", err)
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()

	ticker := time.NewTicker(p.pacingInterval)
	defer ticker.Stop()

	if err := w.Write([]string{
		"Timestamp",
		"SendCount",
	}); err != nil {
		log.Fatalln("error writing field record to file", err)
	}

	writeRecord := func(now time.Time, count int64) {
		if err := w.Write([]string{
			now.Format(RFC3339Milli),
			strconv.FormatInt(count, 10),
		}); err != nil {
			log.Fatalln("error writing record to file", now, err)
		}
	}

	for {
		select {
		case <-p.done:
			return
		case now := <-ticker.C:
			sendCount := int64(0)
			budget := p.getBudget(now)
			for budget > 0 {
				next := p.popItem()
				if next == nil {
					break
				}
				writer, err := p.getStreamWriter(next.header.SSRC)
				if err != nil {
					p.pool.Put(next.payload)
					continue
				}
				n, err := writer.Write(next.header, (*next.payload)[:next.size], next.attributes)
				if err != nil && err != io.ErrClosedPipe {
					p.log.Errorf("failed to write packet: %v", err)
				}
				sendCount += int64(n)
				budget -= next.size + next.header.MarshalSize()
				p.pool.Put(next.payload)
			}
			writeRecord(now, sendCount)
		}
	}
}

func (p *Interceptor) getStreamPriority(ssrc uint32) (int, error) {
	p.ssrcLock.RLock()
	defer p.ssrcLock.RUnlock()
	if prio, ok := p.ssrcToPriority[ssrc]; ok {
		return prio, nil
	}
	return priorityLevelMax, errPacerStreamNotFound
}

func (p *Interceptor) getStreamWriter(ssrc uint32) (interceptor.RTPWriter, error) {
	p.ssrcLock.RLock()
	defer p.ssrcLock.RUnlock()
	if writer, ok := p.ssrcToWriter[ssrc]; ok {
		return writer, nil
	}
	return nil, errPacerStreamNotFound
}

func (p *Interceptor) pushItem(i *item, prio int) {
	p.pqLock.Lock()
	defer p.pqLock.Unlock()
	p.priorityQueue[prio].PushBack(i)
	p.pqSize += i.size + i.header.MarshalSize()
}

func (p *Interceptor) popItem() *item {
	p.pqLock.Lock()
	defer p.pqLock.Unlock()
	for _, l := range p.priorityQueue {
		if l.Len() > 0 {
			if i, ok := l.Remove(l.Front()).(*item); ok {
				p.pqSize -= i.size + i.header.MarshalSize()
				return i
			}
		}
	}
	return nil
}

func (p *Interceptor) getBudget(now time.Time) int {
	p.pqLock.RLock()
	defer p.pqLock.RUnlock()
	getQueueDuration := func(now time.Time) time.Duration {
		firstItemTime := now
		for _, l := range p.priorityQueue {
			if l.Len() > 0 {
				if item, ok := l.Front().Value.(*item); ok {
					if item.timestamp.Before(firstItemTime) {
						firstItemTime = item.timestamp
					}
				}
			}
		}
		return now.Sub(firstItemTime)
	}
	budget := p.pqSize/int(p.maxSendDelay/p.pacingInterval) + p.pqSize%int(p.maxSendDelay/p.pacingInterval)
	if budget < p.latestBudget && getQueueDuration(now) > p.maxSendDelay/2 {
		return p.latestBudget
	}
	p.latestBudget = budget
	return budget
}

// get priority level from stream infomation.
// Audio > Retransmissions > Video and FEC > Padding
func getPriorityLevelFromStreamInfo(info *interceptor.StreamInfo) int {
	p := 0
	if strings.HasPrefix(info.MimeType, "audio/") {
		return p
	}
	p++
	if strings.HasSuffix(info.MimeType, "/rtx") {
		return p
	}
	p++
	if strings.HasPrefix(info.MimeType, "video/") {
		return p
	}
	p++
	return p
}
