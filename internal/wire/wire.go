package wire

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	Hello byte = iota + 1
	Ready
	Data
	Resize
	Exit
	Failure
	MaxPayload = 32 * 1024
)

func Read(r io.Reader) (byte, []byte, error) {
	var h [5]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(h[1:])
	if h[0] < Hello || h[0] > Failure || n > MaxPayload {
		return 0, nil, errors.New("invalid frame")
	}
	b := make([]byte, int(n))
	_, err := io.ReadFull(r, b)
	return h[0], b, err
}
func Write(w io.Writer, kind byte, b []byte) error {
	if kind < Hello || kind > Failure || len(b) > MaxPayload {
		return errors.New("invalid frame")
	}
	var h [5]byte
	h[0] = kind
	binary.BigEndian.PutUint32(h[1:], uint32(len(b)))
	if err := writeAll(w, h[:]); err != nil {
		return err
	}
	return writeAll(w, b)
}
func writeAll(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, err := w.Write(b)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	return nil
}
