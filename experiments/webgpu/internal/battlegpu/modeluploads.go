package battlegpu

import (
	"os"
	"runtime"

	"github.com/gogpu/wgpu"
)

// The pinned native staging belt reuses completed 256 KiB chunks. Larger
// writes use one-off buffers. Splitting changes only copy command boundaries;
// the existing queue owns copied data and destinations through completion.
// Immutable mesh/UV uploads retain their original load-time path.
const modelUploadChunkBytes = 256 << 10

func chunkedModelUploadsEnabled(d *wgpu.Device) bool {
	if os.Getenv("NANOLATHE_WEBGPU_CHUNKED_UPLOADS") != "1" || runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		return false
	}
	contract, ok := any(d).(interface{ CompletionSafeUploads() bool })
	return ok && contract.CompletionSafeUploads()
}

type modelBufferWriter interface {
	WriteBuffer(*wgpu.Buffer, uint64, []byte) error
}

func writeModelUpload(q modelBufferWriter, b *wgpu.Buffer, data []byte, chunked bool) (writes int, err error) {
	if !chunked || len(data) <= modelUploadChunkBytes {
		return 1, q.WriteBuffer(b, 0, data)
	}
	for offset := 0; offset < len(data); offset += modelUploadChunkBytes {
		end := min(offset+modelUploadChunkBytes, len(data))
		writes++
		if err = q.WriteBuffer(b, uint64(offset), data[offset:end]); err != nil {
			return writes, err
		}
	}
	return writes, nil
}

func (m *Models) writeMutableBuffer(b *wgpu.Buffer, data []byte) error {
	writes, err := writeModelUpload(m.q, b, data, m.chunkedUploads)
	m.counters.UploadWrites += writes
	if m.chunkedUploads && len(data) > modelUploadChunkBytes {
		m.counters.ChunkedUploadBytes += len(data)
	}
	return err
}
