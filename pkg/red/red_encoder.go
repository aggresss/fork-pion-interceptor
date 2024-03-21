package red

// Reference: https://www.rfc-editor.org/rfc/rfc2198.html

/*
    0                   1                    2                   3
    0 1 2 3 4 5 6 7 8 9 0 1 2 3  4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |V=2|P|X| CC=0  |M|      PT     |   sequence number of primary  |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |              timestamp  of primary encoding                   |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |           synchronization source (SSRC) identifier            |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |1| block PT=7  |  timestamp offset         |   block length    |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
   |0| block PT=5  |                                               |
   +-+-+-+-+-+-+-+-+                                               +
   |                                                               |
   +                LPC encoded redundant data (PT=7)              +
   |                (14 bytes)                                     |
   +                                               +---------------+
   |                                               |               |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+               +
   |                                                               |
   +                                                               +
   |                                                               |
   +                                                               +
   |                                                               |
   +                                                               +
   |                DVI4 encoded primary data (PT=5)               |
   +                (84 bytes, not to scale)                       +
   /                                                               /
   +                                                               +
   |                                                               |
   +                                                               +
   |                                                               |
   +                                               +---------------+
   |                                               |
   +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
*/

import (
	"container/list"
	"errors"
	"sync"

	"github.com/pion/rtp"
)

const (
	redMTUMax                   = 1200
	redTimestampDeltaMax uint32 = (2 << 14) - 1
)

var (
	errRedPoolCastFailed = errors.New("failed to access red encoder pool, cast failed")
)

type item struct {
	sequeceNumber uint16
	timestamp     uint32
	payload       *[]byte
	size          int
}

type RedEncoder struct {
	payloadType uint8
	pool        *sync.Pool
	store       *list.List
	mutex       sync.RWMutex
}

func NewRedEncoder(payloadType uint8) *RedEncoder {
	e := &RedEncoder{
		payloadType: payloadType,
		store:       list.New(),
	}

	e.pool = &sync.Pool{
		New: func() interface{} {
			b := make([]byte, 1460)
			return &b
		},
	}

	return e
}

func (r *RedEncoder) EncodeRed(header *rtp.Header, payload []byte, redLevel int) (*rtp.Header, []byte, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	redHeader := header.Clone()
	redHeader.PayloadType = r.payloadType
	redPayload := make([]byte, 1460)
	redPayloadSize := len(payload)
	sequeceBase := header.SequenceNumber
	iter := r.store.Back()
	// Check continuity / compound payload size / timestamp delta.
	for i := 0; i < redLevel; i++ {
		sequeceBase--
		if iter == nil {
			redLevel = i
			break
		}
		item := iter.Value.(*item)
		redPayloadSize += item.size
		var timestampDelta uint32 = redHeader.Timestamp - item.timestamp
		if item.sequeceNumber != sequeceBase || redPayloadSize > redMTUMax || timestampDelta > redTimestampDeltaMax {
			redLevel = i
			break
		}
		iter = iter.Prev()
	}
	if iter == nil {
		iter = r.store.Front()
	} else {
		iter = iter.Next()
	}
	iterBak := iter
	offset := 0
	// Encode redundant payload.
	for i := 0; i < redLevel; i++ {
		item := iter.Value.(*item)
		timestampDelta := uint32(header.Timestamp - item.timestamp)
		redPayload[offset] = header.PayloadType | 0b10000000
		redPayload[offset+1] = uint8(timestampDelta >> 6)
		redPayload[offset+2] = (uint8(timestampDelta<<2) & 0b11111100) | ((uint8(item.size >> 8)) & 0b00000011)
		redPayload[offset+3] = uint8(item.size)
		offset += 4
		iter = iter.Next()
	}
	redPayload[offset] = header.PayloadType & 0b01111111
	offset += 1
	iter = iterBak
	for i := 0; i < redLevel; i++ {
		item := iter.Value.(*item)
		copy(redPayload[offset:], (*item.payload)[:item.size])
		offset += item.size
		iter = iter.Next()
	}
	copy(redPayload[offset:], payload)
	offset += len(payload)
	// Store packet.
	if err := r.storePacket(header, payload); err != nil {
		return nil, nil, err
	}

	return &redHeader, redPayload[:offset], nil
}

func (r *RedEncoder) storePacket(header *rtp.Header, payload []byte) error {
	buf, ok := r.pool.Get().(*[]byte)
	if !ok {
		return errRedPoolCastFailed
	}
	copy(*buf, payload)
	i := &item{
		sequeceNumber: header.SequenceNumber,
		timestamp:     header.Timestamp,
		payload:       buf,
		size:          len(payload),
	}
	r.store.PushBack(i)
	for r.store.Len() > redLevelMax {
		if i, ok := r.store.Remove(r.store.Front()).(*item); ok {
			r.pool.Put(i.payload)
		}
	}

	return nil
}
