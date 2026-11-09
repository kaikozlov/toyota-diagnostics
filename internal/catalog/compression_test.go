package catalog

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestDocumentCompressionRoundTrip(t *testing.T) {
	raw := []byte(`{"name":"温度","integer":18446744073709551615,"rows":[1,2,3]}`)
	writer, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	packed := writer.EncodeAll(raw, nil)
	var legacy bytes.Buffer
	z := zlib.NewWriter(&legacy)
	if _, err = z.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"zstd": packed, "legacy-zlib": legacy.Bytes()} {
		t.Run(name, func(t *testing.T) {
			got, err := inflateDocument(data)
			if err != nil || !bytes.Equal(got, raw) {
				t.Fatalf("%s %v", got, err)
			}
		})
	}
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			got, err := inflateDocument(packed)
			if err != nil || !bytes.Equal(got, raw) {
				t.Errorf("concurrent decode: %v", err)
			}
		}()
	}
	workers.Wait()
	if _, err = inflateDocument(packed[:len(packed)-1]); err == nil {
		t.Fatal("truncated zstd frame accepted")
	}
	corrupt := append([]byte(nil), packed...)
	corrupt[len(corrupt)-1] ^= 0xff
	if _, err = inflateDocument(corrupt); err == nil {
		t.Fatal("corrupt zstd frame accepted")
	}
}

func TestDocumentCompressionAllocationBound(t *testing.T) {
	// Single-segment frame with an eight-byte advertised content size. The
	// oversized claim must be rejected before allocating or reading its body.
	frame := make([]byte, 16)
	copy(frame, []byte{0x28, 0xb5, 0x2f, 0xfd, 0xe0})
	binary.LittleEndian.PutUint64(frame[5:13], maxDecodedDocumentSize+1)
	frame[13] = 1
	_, err := inflateDocument(frame)
	if !errors.Is(err, zstd.ErrDecoderSizeExceeded) {
		t.Fatalf("oversized frame: %v", err)
	}
}
