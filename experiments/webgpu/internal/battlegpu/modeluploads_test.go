package battlegpu

import (
	"bytes"
	"errors"
	"testing"

	"github.com/gogpu/wgpu"
)

type uploadRecorder struct {
	dst           []byte
	calls, failAt int
	failure       error
}

func (q *uploadRecorder) WriteBuffer(_ *wgpu.Buffer, at uint64, data []byte) error {
	q.calls++
	if q.calls == q.failAt {
		return q.failure
	}
	copy(q.dst[int(at):], data)
	return nil
}

func TestModelUploadPreservesBytesAcrossChunkBoundaries(t *testing.T) {
	for _, size := range []int{0, 4, modelUploadChunkBytes, modelUploadChunkBytes + 4, 2*modelUploadChunkBytes + 80} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i*17 + 3)
		}
		for _, chunked := range []bool{false, true} {
			q := &uploadRecorder{dst: bytes.Repeat([]byte{0xcd}, size+16)}
			_, err := writeModelUpload(q, nil, data, chunked)
			if err != nil || !bytes.Equal(q.dst[:size], data) || !bytes.Equal(q.dst[size:], bytes.Repeat([]byte{0xcd}, 16)) {
				t.Fatalf("size%d chunked%v: upload changed payload or trailing bytes: %v", size, chunked, err)
			}
		}
	}
}

// Renderer writes keep their destination offset across chunks.
func TestBufferChunksKeepOffset(t *testing.T) {
	data := make([]byte, 2*modelUploadChunkBytes+12)
	for i := range data {
		data[i] = byte(i*29 + 1)
	}
	q := &uploadRecorder{dst: bytes.Repeat([]byte{0xcd}, len(data)+64)}
	writes, err := writeBufferChunks(q, nil, 32, data, true)
	if err != nil || writes != 3 || !bytes.Equal(q.dst[32:32+len(data)], data) || !bytes.Equal(q.dst[:32], bytes.Repeat([]byte{0xcd}, 32)) || !bytes.Equal(q.dst[32+len(data):], bytes.Repeat([]byte{0xcd}, 32)) {
		t.Fatalf("offset chunks: writes%d err%v", writes, err)
	}
}

func TestModelUploadStopsAtFirstFailedChunk(t *testing.T) {
	failure := errors.New("staging write refused")
	data := bytes.Repeat([]byte{0xab}, 3*modelUploadChunkBytes)
	q := &uploadRecorder{dst: bytes.Repeat([]byte{0xcd}, len(data)), failAt: 2, failure: failure}
	writes, err := writeModelUpload(q, nil, data, true)
	if !errors.Is(err, failure) || writes != 2 || q.calls != 2 {
		t.Fatalf("failure lost or later chunk submitted: writes%d calls%d err%v", writes, q.calls, err)
	}
	if !bytes.Equal(q.dst[:modelUploadChunkBytes], data[:modelUploadChunkBytes]) || !bytes.Equal(q.dst[modelUploadChunkBytes:], bytes.Repeat([]byte{0xcd}, 2*modelUploadChunkBytes)) {
		t.Fatal("failed upload modified data beyond its completed prefix")
	}
}
