package catalog

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// Stored documents and parts decode below this bound. A corrupt frame must
// not be able to request an unbounded allocation from its advertised size.
const maxDecodedDocumentSize = 64 << 20

var documentDecoder = sync.OnceValues(func() (*zstd.Decoder, error) {
	return zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderLowmem(true), zstd.WithDecoderMaxMemory(maxDecodedDocumentSize))
})

var zlibReaders sync.Pool

func inflateDocument(data []byte) ([]byte, error) {
	if len(data) >= 4 && data[0] == 0x28 && data[1] == 0xb5 && data[2] == 0x2f && data[3] == 0xfd {
		decoder, err := documentDecoder()
		if err != nil {
			return nil, err
		}
		// DecodeAll is concurrency-safe; the decoder bounds concurrent workspace.
		return decoder.DecodeAll(data, nil)
	}
	// Small blobs (most document parts) are stored as zlib. Readers are
	// pooled because each one allocates a full inflate window.
	src := bytes.NewReader(data)
	r, ok := zlibReaders.Get().(io.ReadCloser)
	var err error
	if ok {
		err = r.(zlib.Resetter).Reset(src, nil)
	} else {
		r, err = zlib.NewReader(src)
	}
	if err != nil {
		return nil, err
	}
	defer zlibReaders.Put(r)
	raw, err := io.ReadAll(io.LimitReader(r, maxDecodedDocumentSize+1))
	if err == nil && len(raw) > maxDecodedDocumentSize {
		return nil, errors.New("stored document exceeds size limit")
	}
	return raw, err
}
