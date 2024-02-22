// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package flexfec

import (
	"strings"
	"sync"

	"github.com/pion/interceptor"
	"github.com/pion/rtp"
)

// FecInterceptor implements FlexFec.
type FecInterceptor struct {
	interceptor.NoOp
	minNumMediaPackets uint32

	streams   map[uint32]*fecStream
	streamsMu sync.Mutex
}

type fecStream struct {
	flexFecEncoder FlexEncoder
	packetBuffer   []rtp.Packet

	fecRtpWriter interceptor.RTPWriter
}

// FecOption can be used to set initial options on Fec encoder interceptors.
type FecOption func(d *FecInterceptor) error

// FecInterceptorFactory creates new FecInterceptors.
type FecInterceptorFactory struct {
	opts []FecOption
}

// NewFecInterceptor returns a new Fec interceptor factory.
func NewFecInterceptor(opts ...FecOption) (*FecInterceptorFactory, error) {
	return &FecInterceptorFactory{opts: opts}, nil
}

// NewInterceptor constructs a new FecInterceptor.
func (r *FecInterceptorFactory) NewInterceptor(_ string) (interceptor.Interceptor, error) {
	// Hardcoded for now:
	// Min num media packets to encode FEC -> 5
	// Min num fec packets -> 1

	i := &FecInterceptor{
		minNumMediaPackets: 5,
		streams:            map[uint32]*fecStream{},
	}

	for _, opt := range r.opts {
		if err := opt(i); err != nil {
			return nil, err
		}
	}

	return i, nil
}

// BindLocalStream lets you modify any outgoing RTP packets. It is called once for per LocalStream. The returned method
// will be called once per rtp packet.
func (r *FecInterceptor) BindLocalStream(info *interceptor.StreamInfo, writer interceptor.RTPWriter) interceptor.RTPWriter {
	// Chromium supports version flexfec-03 of existing draft, this is the one we will configure by default
	// although we should support configuring the latest (flexfec-20) as well.
	r.streamsMu.Lock()
	defer r.streamsMu.Unlock()

	if info.Attributes.Get("flexfec-03") != nil {
		f := &fecStream{
			packetBuffer: make([]rtp.Packet, 0),
		}
		r.streams[info.SSRC] = f

		return interceptor.RTPWriterFunc(func(header *rtp.Header, payload []byte, attributes interceptor.Attributes) (int, error) {
			// Store media RTP packet
			if f.fecRtpWriter != nil {
				f.packetBuffer = append(f.packetBuffer, rtp.Packet{
					Header:  *header,
					Payload: payload,
				})
			}
			// Send the media RTP packet
			result, err := writer.Write(header, payload, attributes)
			// Send the FEC packets
			var fecPackets []rtp.Packet
			if len(f.packetBuffer) == int(r.minNumMediaPackets) && f.fecRtpWriter != nil {
				fecPackets = f.flexFecEncoder.EncodeFec(f.packetBuffer, 2)

				for _, fecPacket := range fecPackets {
					fecResult, fecErr := f.fecRtpWriter.Write(&fecPacket.Header, fecPacket.Payload, attributes)

					if fecErr != nil && fecResult == 0 {
						break
					}
				}
				// Reset the packet buffer now that we've sent the corresponding FEC packets.
				f.packetBuffer = nil
			}

			return result, err
		})
	} else if strings.HasSuffix(info.MimeType, "/flexfec-03") && info.Attributes.Get("apt_ssrc") != nil {
		if stream, ok := r.streams[info.Attributes.Get("apt_ssrc").(uint32)]; ok {
			stream.flexFecEncoder = NewFlexEncoder03(info.PayloadType, info.SSRC)
			stream.fecRtpWriter = writer
		}
	}

	return writer
}

// UnbindLocalStream is called when the Stream is removed. It can be used to clean up any data related to that track.
func (r *FecInterceptor) UnbindLocalStream(info *interceptor.StreamInfo) {
	r.streamsMu.Lock()
	delete(r.streams, info.SSRC)
	r.streamsMu.Unlock()
}
