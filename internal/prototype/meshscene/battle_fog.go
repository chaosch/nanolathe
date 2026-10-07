package meshscene

import (
	"fmt"
	"strings"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
	"github.com/nanolathe-gg/nanolathe/internal/world"
)

// Freeze map-load height bytes before the worker starts. GPU height sampling is
// a display approximation; per-instance receivers retain the integer two-stage
// terrain query on these immutable samples [03 §2.3]. Rect places sample centres
// at 0,16,...; its outer edges extend half a cell beyond those centres.
func battleHeightField(t *world.Terrain) HeightField {
	if t == nil || t.CellW <= 0 || t.CellH <= 0 {
		return HeightField{}
	}
	f := HeightField{Width: int(t.CellW), Height: int(t.CellH), Values: make([]float32, int(t.CellW*t.CellH)), Rect: [4]float32{-8, -8, float32(t.CellW)*16 - 8, float32(t.CellH)*16 - 8}}
	for i, p := range t.Plot {
		if i >= len(f.Values) {
			break
		}
		f.Values[i] = float32(p.Height())
	}
	return f
}
func battleGroundHeight(f HeightField, x, z numeric.Fixed) float32 {
	cx, cz := world.WorldToCell(x), world.WorldToCell(z)
	if cx < 0 || cz < 0 || int(cx+1) >= f.Width || int(cz+1) >= f.Height {
		return -1.0 / 65536
	}
	fx := int32((int64(x) - int64(world.CellToWorld(cx))) / 65536)
	fz := int32((int64(z) - int64(world.CellToWorld(cz))) / 65536)
	base := int(cz)*f.Width + int(cx)
	step := func(a, b, fraction int32) int32 {
		d := (b - a) * fraction
		if d < 0 {
			d += 15
		}
		return a + (d >> 4)
	}
	return float32(step(step(int32(f.Values[base]), int32(f.Values[base+1]), fx), step(int32(f.Values[base+f.Width]), int32(f.Values[base+f.Width+1]), fx), fz))
}

func (s *battleSource) prepareFog() error {
	if s.spectator {
		return nil
	}
	gaf, err := formats.LoadGAFFile(s.fs, "anims/fog.gaf")
	if err != nil {
		return err
	}
	for ei := range gaf.Entries {
		entry := &gaf.Entries[ei]
		for i := 0; i < 4; i++ {
			if strings.EqualFold(entry.Name, fmt.Sprintf("Gray%d", i+1)) {
				s.fogGray[i] = entry
			}
			if strings.EqualFold(entry.Name, fmt.Sprintf("Black%d", i+1)) {
				s.fogBlack[i] = entry
			}
		}
	}
	return nil
}

// The native modulation uses authored GAF mask geometry and world-anchored
// variants [03 §3.3], sampled every eight world pixels. Current fog multiplies
// RGB by 0.5 rather than performing retail's destination-palette gray lookup;
// this is an explicit low-resolution prototype approximation, not LOS smoothing.
func (s *battleSource) appendFog(cur *frame.Frame, old *battlePublication) FogFrame {
	if s.spectator || cur == nil || !cur.Fog.Valid {
		return FogFrame{}
	}
	src := cur.Fog
	if old != nil && src.Source != 0 && old.fogSource == src.Source && old.fogVersion == src.Version {
		return old.frame.Fog
	}
	if src.W <= 0 || src.H <= 0 || int64(src.W)*int64(src.H) != int64(len(src.Ch0)) || len(src.Ch1) != len(src.Ch0) {
		return FogFrame{}
	}
	const sample = 8
	const samplesPerCell = 32 / sample
	dst := FogFrame{Width: int(src.W) * samplesPerCell, Height: int(src.H) * samplesPerCell, Rect: [4]float32{float32(src.OriginX*32 + 16), float32(src.OriginZ*32 + 16), float32((src.OriginX+src.W)*32 + 16), float32((src.OriginZ+src.H)*32 + 16)}}
	dst.RGBA = make([]byte, dst.Width*dst.Height*4)
	for i := range dst.RGBA {
		dst.RGBA[i] = 255
	}
	fill := func(col, row int, shade byte) {
		for y := row * samplesPerCell; y < (row+1)*samplesPerCell; y++ {
			for x := col * samplesPerCell; x < (col+1)*samplesPerCell; x++ {
				i := (y*dst.Width + x) * 4
				dst.RGBA[i], dst.RGBA[i+1], dst.RGBA[i+2] = shade, shade, shade
			}
		}
	}
	mask := func(entry *formats.GAFEntry, nibble byte, col, row int, shade byte) {
		if entry == nil || len(entry.Frames) == 0 {
			return
		}
		fi := min(int(nibble)-1, len(entry.Frames)-1)
		f := entry.Frames[fi].Frame
		if f == nil {
			return
		}
		left, top := col*32-int(f.XOffset), row*32-int(f.YOffset)
		for y := max(0, (top-sample/2+sample-1)/sample); y < dst.Height && y*sample+sample/2 < top+int(f.Height); y++ {
			for x := max(0, (left-sample/2+sample-1)/sample); x < dst.Width && x*sample+sample/2 < left+int(f.Width); x++ {
				ix, iy := x*sample+sample/2-left, y*sample+sample/2-top
				if ix < 0 || iy < 0 {
					continue
				}
				p := iy*int(f.Width) + ix
				if p >= len(f.Pixels) || (p < len(f.Transparent) && f.Transparent[p]) {
					continue
				}
				i := (y*dst.Width + x) * 4
				for k := 0; k < 3; k++ {
					dst.RGBA[i+k] = byte(uint16(dst.RGBA[i+k]) * uint16(shade) / 255)
				}
			}
		}
	}
	for row := 0; row < int(src.H); row++ {
		for col := 0; col < int(src.W); col++ {
			i := row*int(src.W) + col
			black, gray := src.Ch0[i], src.Ch1[i]
			if black == 15 {
				fill(col, row, 0)
				continue
			}
			variant := int((int32(col) + src.OriginX + int32(row) + src.OriginZ + 2) & 3)
			if gray == 15 {
				fill(col, row, 128)
			} else if gray != 0 {
				mask(s.fogGray[variant], gray, col, row, 128)
			}
			if black != 0 {
				mask(s.fogBlack[variant], black, col, row, 0)
			}
		}
	}
	return dst
}
