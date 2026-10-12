package battlegpu

import "testing"

func TestStorageReservationIsBoundedAndRetainsGrowth(t *testing.T) {
	if modelStorageReserveBytes[0] != 2*modelStorageReserveBytes[1] {
		t.Fatal("endpoint reservation must hold both pose endpoints")
	}
	total := 0
	for _, n := range modelStorageReserveBytes {
		if n <= 0 || n > modelCoreBufferLimit || n&(n-1) != 0 {
			t.Fatalf("invalid geometric reservation %d", n)
		}
		total += n
	}
	l := Layers{}
	for _, record := range []struct {
		buffer *layerBuffer
		stride int
	}{{&l.sprites, 96}, {&l.annotations, 64}, {&l.overlayQuads, 64}, {&l.effectQuads, 64}, {&l.effectVertices, layerDirectVertexStride}, {&l.effectGround, 80}, {&l.effectSmoke, layerSmokeStride}, {&l.lights, 64}} {
		floor := l.storageFloor(record.buffer)
		if floor < record.stride || floor > modelCoreBufferLimit {
			t.Fatalf("invalid layer floor %d", floor)
		}
		size := record.stride
		for size < floor {
			size *= 2
		}
		if size >= floor*2 {
			t.Fatalf("reservation overallocates its geometric tier: %d >= %d", size, floor*2)
		}
		total += size
	}
	total += glowStorageReserveBytes
	if total > 24<<20 {
		t.Fatalf("combined mutable buffer reservation exceeds 24 MiB: %d", total)
	}
	// The same geometric allocation continues above the reservation; no fixed
	// maximum, truncation or alternate UV/instance representation is introduced.
	if l.storageFloor(&layerBuffer{}) != 0 {
		t.Fatal("unrecognized resource receives an implicit floor")
	}
}
