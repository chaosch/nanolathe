package battlegpu

import (
	"os"

	"github.com/gogpu/wgpu"
)

// Stock wgpu stages every buffer write, reusing completed 256 KiB belt chunks
// but allocating (and later destroying) a one-off mapped staging buffer for
// each larger write. Splitting writes at the chunk size keeps them on reused
// chunks: the mutable model records first (99.9 to 111.9 Hz on the zoomed-out
// 1440p field), now every renderer write through deferredQueue. It changes
// only copy boundaries, so it is safe on every backend;
// NANOLATHE_WEBGPU_CHUNKED_UPLOADS=0 is the control. Immutable mesh/UV uploads
// retain their original load-time path.
const modelUploadChunkBytes = 256 << 10

func chunkedModelUploadsEnabled() bool {
	return os.Getenv("NANOLATHE_WEBGPU_CHUNKED_UPLOADS") != "0"
}

type modelBufferWriter interface {
	WriteBuffer(*wgpu.Buffer, uint64, []byte) error
}

func writeModelUpload(q modelBufferWriter, b *wgpu.Buffer, data []byte, chunked bool) (writes int, err error) {
	return writeBufferChunks(q, b, 0, data, chunked)
}

// writeBufferChunks writes data at offset, split at the belt's chunk size
// when chunked, and stops at the first failed write.
func writeBufferChunks(q modelBufferWriter, b *wgpu.Buffer, offset uint64, data []byte, chunked bool) (writes int, err error) {
	if !chunked || len(data) <= modelUploadChunkBytes {
		return 1, q.WriteBuffer(b, offset, data)
	}
	for at := 0; at < len(data); at += modelUploadChunkBytes {
		end := min(at+modelUploadChunkBytes, len(data))
		writes++
		if err = q.WriteBuffer(b, offset+uint64(at), data[at:end]); err != nil {
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
