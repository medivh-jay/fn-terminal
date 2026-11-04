package wire

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestFramingPreservesSplitUnicodeBytes(t *testing.T) {
	var b bytes.Buffer
	data := []byte("中文🙂")
	if err := Write(&b, Data, data[:4]); err != nil {
		t.Fatal(err)
	}
	if err := Write(&b, Data, data[4:]); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for i := 0; i < 2; i++ {
		kind, p, err := Read(&b)
		if err != nil || kind != Data {
			t.Fatalf("frame %v %v", kind, err)
		}
		got = append(got, p...)
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("corrupt bytes: %q", got)
	}
}
func TestRejectsOversizeAndTruncatedFrames(t *testing.T) {
	var b bytes.Buffer
	b.WriteByte(Data)
	_ = binary.Write(&b, binary.BigEndian, uint32(1<<30))
	if _, _, err := Read(&b); err == nil {
		t.Fatal("accepted oversized allocation")
	}
	if err := Write(&b, Data, make([]byte, MaxPayload+1)); err == nil {
		t.Fatal("accepted oversized output")
	}
	if _, _, err := Read(bytes.NewReader([]byte{Data, 0, 0, 0, 3, 1})); err == nil {
		t.Fatal("accepted truncated input")
	}
}
