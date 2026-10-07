// Package meshscene is the throwaway Metal experiment's asset and pose boundary.
// It is presentation-only and does not replace any gameplay or retail contract.
package meshscene

// Vertex is 64 bytes. Position and normal are model-local, UV is normalized in
// the shared model atlas, and Piece selects the instance's rigid transform.
type Vertex struct {
	Position    [3]float32
	Normal      [3]float32
	UV          [2]float32
	Color       [3]float32
	Piece       uint32
	Material    uint32     // zero uses direct atlas UV; otherwise index into Scene.Materials
	ShadeNormal [3]float32 // unnormalized average of authored retail face normals
}
type Mesh struct {
	Name          string
	Vertices      []Vertex
	Indices       []uint32
	EdgeIndices   []uint32 // original authored edges, independent of face subdivision
	Pieces        int
	SourceFaces   int
	MaterialSlots []ModelMaterialSlot
}
type Texture struct {
	Width, Height int
	RGBA          []byte
}

// Mat4 uses column-major storage, with world x right, y up, z down-screen.
type Mat4 [16]float32

// Phase is presentation-only: grounded subjects, effects, then airborne subjects.
const (
	PhaseGround uint32 = iota
	PhaseEffects
	PhaseAir
)

type Instance struct {
	Materials    []NativeModelMaterial // mesh-local primitive selections; immutable through submission
	CoolingAge   float32               // committed wreck age plus one; zero means no cooling treatment
	Phase        uint32
	Subject      uint64 // keyed carrier and cargo share one cloak resolve
	SubjectOrder uint32 // parent first, then published Cargo order
	Mesh         int
	PoseOffset   int
	Visual       ModelVisual
}
type PoseFrame struct{ Transforms []Mat4 }
type Scene struct {
	Playable                 bool
	OverlayAtlas             Texture
	FogAtlas                 Texture // authored fog masks, followed by the 256-entry palette row
	DitheredFog              bool    // production display option; independent of committed LOS
	Name                     string
	Meshes                   []Mesh
	Atlas                    Texture
	ModelPalette             Texture
	Terrain                  Texture
	WaterMask                Texture // production projected wet/dry/shore mask; zero disables the layer
	WaterMaskStep            int
	WaterLava, WaterDamaging bool
	SpriteAtlas              Texture
	Materials                []Material
	TextureFrames            [][4]float32
	HeightField              HeightField
	Live                     LiveSource // nil retains the original recorded-pose workload
	// Terrain rectangle in world x,z; it is a flat 2D backdrop.
	TerrainRect [4]float32
	Instances   []Instance
	// Fixed topology across frames, sampled at 30 Hz. Matrices include unit world
	// placement and the piece hierarchy. The renderer blends adjacent samples.
	Frames   []PoseFrame
	Camera   [3]float32 // x,z,zoom; screen=(x-camera.x, z-camera.z-y)*zoom+viewport/2
	Metadata map[string]any
}
type LoadOptions struct {
	MaterialSource *ModelMaterialSource
	Root           string
	Map            string
	Units          int
	QuadMultiplier int // subdivide authored faces, not coincident overdraw
	TextureScale   int
	Samples        int
}
