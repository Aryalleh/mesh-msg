// Package transfer implements large logical Mava transfers over packet-sized
// transports. A transfer is independent of LoRa or Meshtastic.
package transfer

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/Aryalleh/mesh-msg/core/packet"
	"time"
)

const (
	Version          byte = 1
	HeaderSize            = 17
	Magic0           byte = 0x4d
	Magic1           byte = 0x56
	MaxChunks             = 65535
	DefaultChunkSize      = 216
	MaxWindow             = 16
)

type Kind byte

const (
	KindData   Kind = 1
	KindAck    Kind = 2
	KindCancel Kind = 3
)

type ID [8]byte

type Chunk struct {
	ID    ID
	Index uint16
	Total uint16
	Flags byte
	Body  []byte
}

func (c Chunk) Encode() ([]byte, error) {
	if c.Total == 0 || c.Index >= c.Total {
		return nil, errors.New("transfer: invalid chunk index/count")
	}
	if len(c.Body) > 65535 {
		return nil, errors.New("transfer: chunk body too large")
	}
	b := make([]byte, HeaderSize+len(c.Body))
	b[0], b[1] = Magic0, Magic1
	b[2] = Version
	b[3] = byte(KindData)
	b[4] = c.Flags
	copy(b[5:13], c.ID[:])
	binary.BigEndian.PutUint16(b[13:15], c.Index)
	binary.BigEndian.PutUint16(b[15:17], c.Total)
	copy(b[17:], c.Body)
	return b, nil
}

func Decode(b []byte) (Chunk, error) {
	var c Chunk
	if len(b) < HeaderSize {
		return c, errors.New("transfer: short envelope")
	}
	if b[0] != Magic0 || b[1] != Magic1 || b[2] != Version || Kind(b[3]) != KindData {
		return c, errors.New("transfer: invalid data envelope")
	}
	copy(c.ID[:], b[5:13])
	c.Index = binary.BigEndian.Uint16(b[13:15])
	c.Total = binary.BigEndian.Uint16(b[15:17])
	c.Flags = b[4]
	if c.Total == 0 || c.Index >= c.Total {
		return c, errors.New("transfer: invalid chunk index/count")
	}
	c.Body = b[17:]
	return c, nil
}

func Split(id ID, body []byte, chunkSize int) ([]Chunk, error) {
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}
	if len(body) == 0 {
		return []Chunk{{ID: id, Index: 0, Total: 1}}, nil
	}
	n := (len(body) + chunkSize - 1) / chunkSize
	if n > MaxChunks {
		return nil, errors.New("transfer: body exceeds 65535 chunks")
	}
	out := make([]Chunk, n)
	for i := range out {
		start := i * chunkSize
		end := start + chunkSize
		if end > len(body) {
			end = len(body)
		}
		out[i] = Chunk{ID: id, Index: uint16(i), Total: uint16(n), Body: body[start:end]}
	}
	return out, nil
}

type Receiver struct {
	ID      ID
	Total   uint16
	Chunks  map[uint16][]byte
	Updated time.Time
}

func NewReceiver(now time.Time) *Receiver {
	return &Receiver{Chunks: map[uint16][]byte{}, Updated: now}
}

func (r *Receiver) Add(c Chunk, now time.Time) (bool, error) {
	if r.Total == 0 {
		r.ID, r.Total = c.ID, c.Total
	}
	if r.ID != c.ID || r.Total != c.Total {
		return false, errors.New("transfer: chunk belongs to another transfer")
	}
	if _, ok := r.Chunks[c.Index]; !ok {
		r.Chunks[c.Index] = append([]byte(nil), c.Body...)
	}
	r.Updated = now
	return len(r.Chunks) == int(r.Total), nil
}

func (r *Receiver) Assemble() ([]byte, error) {
	if r.Total == 0 || len(r.Chunks) != int(r.Total) {
		return nil, errors.New("transfer: incomplete transfer")
	}
	var total int
	for i := uint16(0); i < r.Total; i++ {
		b, ok := r.Chunks[i]
		if !ok {
			return nil, fmt.Errorf("transfer: missing chunk %d", i)
		}
		total += len(b)
	}
	out := make([]byte, 0, total)
	for i := uint16(0); i < r.Total; i++ {
		out = append(out, r.Chunks[i]...)
	}
	return out, nil
}

type Ack struct {
	ID     ID
	Base   uint16
	Bitmap uint16
}

func (a Ack) Encode() []byte {
	b := make([]byte, HeaderSize+2)
	b[0], b[1] = Magic0, Magic1
	b[2] = Version
	b[3] = byte(KindAck)
	copy(b[5:13], a.ID[:])
	binary.BigEndian.PutUint16(b[13:15], a.Base)
	binary.BigEndian.PutUint16(b[15:17], a.Bitmap)
	return b
}
func DecodeAck(b []byte) (Ack, error) {
	var a Ack
	if len(b) < HeaderSize+2 || b[0] != Magic0 || b[1] != Magic1 || b[2] != Version || Kind(b[3]) != KindAck {
		return a, errors.New("transfer: invalid ack")
	}
	copy(a.ID[:], b[5:13])
	a.Base = binary.BigEndian.Uint16(b[13:15])
	a.Bitmap = binary.BigEndian.Uint16(b[15:17])
	return a, nil
}
func (a Ack) Has(index uint16) bool {
	if index < a.Base || index >= a.Base+16 {
		return false
	}
	return a.Bitmap&(1<<uint(index-a.Base)) != 0
}

// MakePacket wraps one transfer chunk in an existing Mava DATA packet.
func MakePacket(c Chunk, dest packet.Hash, maxHops uint8) (*packet.Packet, error) {
	b, err := c.Encode()
	if err != nil {
		return nil, err
	}
	return &packet.Packet{Type: packet.TypeData, MaxHops: maxHops, ID: packet.NewID(), Dest: dest, Body: b}, nil
}
