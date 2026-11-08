package formats

import (
	"encoding/binary"
	"fmt"
	"math"
)

// LoadGAFIsolated 完美隔離加載：保留原廠 RLE 圖案，跳過子幀表防止越界，不合成文字。
func LoadGAFIsolated(data []byte) (*GAF, error) {
	limits := DefaultGAFLimits()
	if len(data) < 12 {
		return nil, fmt.Errorf("gaf: file is too small")
	}

	meta := &GAFMetadata{
		Version:    binary.LittleEndian.Uint32(data[0:4]),
		EntryCount: binary.LittleEndian.Uint32(data[4:8]),
		Unknown:    binary.LittleEndian.Uint32(data[8:12]),
	}

	count := uint64(0)
	if n := int16(meta.EntryCount); n > 0 {
		count = uint64(n)
	}
	if count > uint64((len(data)-12)/4) {
		return nil, fmt.Errorf("gaf: entry offset table is truncated")
	}

	meta.Entries = make([]GAFMetadataEntry, 0, int(count))
	budget := gafMetadataBudget{limits: limits}
	entryCache := make(map[uint32]GAFMetadataEntry)

	for i := uint64(0); i < count; i++ {
		offset := binary.LittleEndian.Uint32(data[12+i*4 : 16+i*4])
		if uint64(offset) > uint64(len(data)) || uint64(len(data))-uint64(offset) < 40 {
			return nil, fmt.Errorf("gaf: entry %d points outside file", i)
		}

		if cached, ok := entryCache[offset]; ok {
			meta.Entries = append(meta.Entries, cached)
			continue
		}

		header := data[offset : uint64(offset)+40]
		nameBytes := header[8:40]
		if nul := indexByte(nameBytes, 0); nul >= 0 {
			nameBytes = nameBytes[:nul]
		}
		entry := GAFMetadataEntry{
			Name:         string(nameBytes),
			FrameCount:   binary.LittleEndian.Uint16(header[0:2]),
			Unknown1:     binary.LittleEndian.Uint16(header[2:4]),
			Unknown2:     binary.LittleEndian.Uint32(header[4:8]),
			sourceOffset: offset,
		}

		refsStart := uint64(offset) + 40
		refBytes := uint64(entry.FrameCount) * 8
		if refsStart > uint64(len(data)) || refBytes > uint64(len(data))-refsStart {
			return nil, fmt.Errorf("gaf: entry %q frame table is truncated", entry.Name)
		}
		if err := budget.addRefs(uint64(entry.FrameCount)); err != nil {
			return nil, fmt.Errorf("gaf: entry %q: %w", entry.Name, err)
		}

		entry.Frames = make([]GAFMetadataFrameRef, entry.FrameCount)
		for frame := range entry.Frames {
			ref := data[refsStart+uint64(frame)*8 : refsStart+uint64(frame+1)*8]
			entry.Frames[frame].Offset = binary.LittleEndian.Uint32(ref[0:4])
			entry.Frames[frame].Value = binary.LittleEndian.Uint32(ref[4:8])

			decoded, err := decodeFrameSkipSubframes(data, entry.Frames[frame].Offset, &budget)
			if err != nil {
				return nil, fmt.Errorf("gaf: entry %q frame %d: %w", entry.Name, frame, err)
			}
			entry.Frames[frame].Frame = decoded
		}
		entryCache[offset] = entry
		meta.Entries = append(meta.Entries, entry)
	}

	gaf := &GAF{
		Version:    meta.Version,
		EntryCount: meta.EntryCount,
		Unknown:    meta.Unknown,
		Entries:    make([]GAFEntry, 0, len(meta.Entries)),
	}

	framesCache := make(map[*GAFMetadataFrame]*GAFFrame)

	for i := range meta.Entries {
		source := &meta.Entries[i]
		entry := GAFEntry{
			Name:       source.Name,
			FrameCount: source.FrameCount,
			Unknown1:   source.Unknown1,
			Unknown2:   source.Unknown2,
			Frames:     make([]GAFFrameRef, len(source.Frames)),
		}
		for j := range source.Frames {
			ref := source.Frames[j]
			frame, err := materializeGAFFrame(data, ref.Frame, framesCache, true)
			if err != nil {
				return nil, err
			}
			entry.Frames[j] = GAFFrameRef{Offset: ref.Offset, Value: ref.Value, Frame: frame}
		}
		gaf.Entries = append(gaf.Entries, entry)
	}

	return gaf, nil
}

// decodeFrameSkipSubframes 是原廠 indexGAFFrame 的精確修正版。
// 它發現了子幀表，但【不讀取它】，而是將偏移量跳過子幀表，直接解壓底圖 RLE。
func decodeFrameSkipSubframes(data []byte, offset uint32, budget *gafMetadataBudget) (*GAFMetadataFrame, error) {
	if uint64(offset) > uint64(len(data)) || uint64(len(data))-uint64(offset) < 24 {
		return nil, fmt.Errorf("frame pointer 0x%x is outside file", offset)
	}

	header := data[offset : uint64(offset)+24]
	frame := &GAFMetadataFrame{
		Width:            binary.LittleEndian.Uint16(header[0:2]),
		Height:           binary.LittleEndian.Uint16(header[2:4]),
		XOffset:          int16(binary.LittleEndian.Uint16(header[4:6])),
		YOffset:          int16(binary.LittleEndian.Uint16(header[6:8])),
		ColorKey:         header[8],
		Compressed:       header[9],
		SubframeCount:    header[10],
		AlternateBlitter: header[11],
		DataOffset:       binary.LittleEndian.Uint32(header[16:20]),
	}

	pixelCount := uint64(frame.Width) * uint64(frame.Height)
	if pixelCount > uint64(math.MaxInt) || pixelCount > maxGAFFramePixels {
		return nil, fmt.Errorf("frame 0x%x is too large", offset)
	}
	if err := budget.addPixels(pixelCount); err != nil {
		return nil, fmt.Errorf("frame 0x%x: %w", offset, err)
	}

	// 【核心修正】：如果存在子幀表，直接跳過它！
	actualDataOffset := frame.DataOffset
	if frame.SubframeCount > 0 {
		actualDataOffset += uint32(frame.SubframeCount) * 4
	}

	// 把 Subframes 清空，不讓它去合成文字
	frame.Subframes = nil
	frame.SubframeCount = 0

	if frame.Compressed == 1 {
		// 使用原廠的 walkGAFRLE，但傳入修正後的偏移量！
		// 這裡為了不直接改原廠 walkGAFRLE 的參數（它只接受 frame），
		// 我們直接把 frame 的 DataOffset 改成修正後的偏移量。
		frame.DataOffset = actualDataOffset
		if err := walkGAFRLE(data, frame, nil, budget); err != nil {
			return nil, fmt.Errorf("frame 0x%x: %w", offset, err)
		}
	} else if frame.Compressed == 0 {
		frame.DataOffset = actualDataOffset
		if uint64(frame.DataOffset) > uint64(len(data)) || pixelCount > uint64(len(data))-uint64(frame.DataOffset) {
			return nil, fmt.Errorf("frame 0x%x raw pixels are truncated", offset)
		}
	}

	return frame, nil
}
