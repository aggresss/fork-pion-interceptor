package playoutdelay

import (
	"encoding/binary"
	"errors"
)

const (
	playoutDelayExtensionSize = 3
)

var (
	errTooSmall = errors.New("buffer too small")
)

// PlayoutDelayExtension is a extension payload format in
// https://webrtc.googlesource.com/src/+/main/docs/native-code/rtp-hdrext/playout-delay/README.md
//
// 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |  ID   | len=2 |       MIN delay       |       MAX delay       |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

type playoutDelayExtension struct {
	minDelay uint16
	maxDelay uint16
}

// Marshal serializes the members to buffer
func (p playoutDelayExtension) Marshal() ([]byte, error) {
	return []byte{
		byte(p.minDelay & 0x0FF0 >> 4),
		byte((p.minDelay & 0x000F << 4) | (p.maxDelay & 0x0F00 >> 8)),
		byte(p.maxDelay & 0x00FF),
	}, nil
}

// Unmarshal parses the passed byte slice and stores the result in the members
func (p *playoutDelayExtension) Unmarshal(rawData []byte) error {
	if len(rawData) < playoutDelayExtensionSize {
		return errTooSmall
	}

	p.minDelay = binary.BigEndian.Uint16(rawData[0:2]) >> 4
	p.maxDelay = binary.BigEndian.Uint16(rawData[1:3]) & 0x0FFF
	return nil
}
