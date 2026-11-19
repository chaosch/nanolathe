package formats

import "strings"

func materializeGAFFrameNoneText(data []byte, meta *GAFMetadataFrame, cache map[*GAFMetadataFrame]*GAFFrame, keepDirectRaster bool, commands []string, name string) (*GAFFrame, error) {
	if frame, ok := cache[meta]; ok {
		return frame, nil
	}
	pixelCount := int(meta.Width) * int(meta.Height)
	frame := &GAFFrame{Width: meta.Width, Height: meta.Height, XOffset: meta.XOffset, YOffset: meta.YOffset, ColorKey: meta.ColorKey, Compressed: meta.Compressed, Unknown2: meta.Unknown2, DataOffset: meta.DataOffset, Unknown3: meta.Unknown3, SubframeCount: meta.SubframeCount, AlternateBlitter: meta.AlternateBlitter, compositeDepth: meta.compositeDepth}
	frame.Pixels = make([]byte, pixelCount)
	frame.Transparent = make([]bool, pixelCount)
	frame.PlainPixels = frame.Pixels
	frame.PlainTransparent = frame.Transparent
	cache[meta] = frame
	if meta.SubframeCount != 0 {
		// TODO(question): retail's ordinary loader relocates direct children;
		// establish whether authored nested child tables are supported before
		// treating recursive host materialization as a retail layout contract.
		// TODO(question): trace remaining feature-mask and structure-texture
		// consumers before replacing their ordinary-only compatibility raster.
		// The established fog, glyph and direct readers use their own paths
		// [02 R-MALF-01 §6].
		for i := range frame.Transparent {
			frame.Transparent[i] = true
		}
		frame.Subframes = make([]*GAFFrame, len(meta.Subframes))
		for i, childMeta := range meta.Subframes {
			child, err := materializeGAFFrameNoneText(data, childMeta, cache, keepDirectRaster, commands, name)
			if err != nil {
				return nil, err
			}
			frame.Subframes[i] = child
			if child.AlternateBlitter != 0 {
				continue
			}
			if i == 1 && commandButtonName(name, commands) != "" {
				continue
			}
			compositeGAFPlainNoneText(frame, child)
		}
		return frame, nil
	}
	if meta.Compressed == 0 {
		copy(frame.Pixels, data[meta.DataOffset:uint64(meta.DataOffset)+uint64(pixelCount)])
		for i, pixel := range frame.Pixels {
			if pixel == frame.ColorKey {
				frame.Transparent[i] = true
			}
		}
		return frame, nil
	}
	// A direct consumer sees the encoded bytes, even on an RLE frame. Preserve
	// at most one raster under the existing per-frame geometry budget. A short
	// file span is rejected by DirectRaster, without rejecting ordinary decode.
	if end := uint64(meta.DataOffset) + uint64(pixelCount); keepDirectRaster && end <= uint64(len(data)) {
		view := *frame
		view.Compressed = 0
		view.Pixels = append([]byte(nil), data[meta.DataOffset:end]...)
		view.Transparent, view.PlainPixels, view.PlainTransparent = nil, nil, nil
		frame.directRaster = &view
	}
	if err := walkGAFRLE(data, meta, frame, nil); err != nil {
		return nil, err
	}
	return frame, nil
}
func LoadGAFNoneText(data []byte, commands []string) (*GAF, error) {
	return LoadGAFWithLimitsNoneText(data, DefaultGAFLimits(), commands)
}

// LoadGAFWithLimits decodes an animation bank under explicit host-safety
// budgets [fmt gaf].
func LoadGAFWithLimitsNoneText(data []byte, limits GAFLimits, commands []string) (*GAF, error) {
	meta, err := LoadGAFMetadataWithLimits(data, limits)
	if err != nil {
		return nil, err
	}
	return materializeGAFNoneText(data, meta, true, commands)
}
func materializeGAFNoneText(data []byte, meta *GAFMetadata, keepDirectRaster bool, commands []string) (*GAF, error) {
	gaf := &GAF{Version: meta.Version, EntryCount: meta.EntryCount, Unknown: meta.Unknown, Entries: make([]GAFEntry, 0, len(meta.Entries))}
	entries := make(map[uint32]GAFEntry)
	frames := make(map[*GAFMetadataFrame]*GAFFrame)
	for i := range meta.Entries {
		source := &meta.Entries[i]
		if cached, ok := entries[source.sourceOffset]; ok {
			gaf.Entries = append(gaf.Entries, cached)
			continue
		}
		entry := GAFEntry{Name: source.Name, FrameCount: source.FrameCount, Unknown1: source.Unknown1, Unknown2: source.Unknown2, Frames: make([]GAFFrameRef, len(source.Frames))}
		for j := range source.Frames {
			ref := source.Frames[j]
			frame, err := materializeGAFFrameNoneText(data, ref.Frame, frames, keepDirectRaster, commands, source.Name)
			if err != nil {
				return nil, err
			}
			entry.Frames[j] = GAFFrameRef{Offset: ref.Offset, Value: ref.Value, Frame: frame}
		}
		entries[source.sourceOffset] = entry
		gaf.Entries = append(gaf.Entries, entry)
	}
	return gaf, nil
}

// commandButtonName returns the table row a gadget belongs to, or "" when it is
// not a command button. The longest matching suffix wins, which is what keeps
// ARMUNLOAD out of the LOAD row and ARMMOVEORD out of the MOVE row.
func commandButtonName(gadget string, commands []string) string {
	upper := strings.ToUpper(gadget)
	best := ""
	for _, name := range commands {
		if len(name) > len(best) && strings.HasSuffix(upper, name) {
			best = name
		}
	}
	return best
}

func compositeGAFPlainNoneText(parent, child *GAFFrame) {
	dx := int(parent.XOffset) - int(child.XOffset)
	dy := int(parent.YOffset) - int(child.YOffset)
	for sy := 0; sy < int(child.Height); sy++ {
		dyPos := dy + sy
		if dyPos < 0 || dyPos >= int(parent.Height) {
			continue
		}
		for sx := 0; sx < int(child.Width); sx++ {
			dxPos := dx + sx
			if dxPos < 0 || dxPos >= int(parent.Width) {
				continue
			}
			subIndex := sy*int(child.Width) + sx
			if child.PlainTransparent[subIndex] {
				continue
			}
			index := dyPos*int(parent.Width) + dxPos
			parent.PlainPixels[index] = child.PlainPixels[subIndex]
			parent.PlainTransparent[index] = false
		}
	}
}
