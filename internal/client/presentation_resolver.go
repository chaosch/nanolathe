package client

// Presentation-only resolution at the active Ebitengine frame boundary.
// Authored projectile/effect assets whose archive route is not published stay
// unresolved; choosing a guessed archive/entry would make the image look
// plausible while violating the clean-room contract [03 §5.4][I9].

import (
	"strings"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/internal/frame"
	"github.com/nanolathe-gg/nanolathe/internal/render"
	"github.com/nanolathe-gg/nanolathe/internal/sim/numeric"
)

const projectileGAFPath = "anims/" + projectileGAFBank + ".gaf"

// projectileGAFBank is the projectile bank's name among the anims banks.
const projectileGAFBank = "fx"

// WarmProjectileArt loads the projectile bank off the draw path, beside the
// battle's other presentation art (WarmBattleFeatureSequences). Loaded on
// first use instead, the first shot a battle or settings preview drew decoded
// the whole bank inside that frame. Presentation state only [I6].
func (c *Client) WarmProjectileArt() {
	c.ensureProjectileGAF()
}

// The fixed engine slots are the only shared projectile GAF identities closed
// by the retail contract. Selector 4 intentionally binds the same `plasmasm`
// entry as selector 1 [06 R-WFX-01 §1][06 R-WFX-01 §4].
var projectileSelectorSequences = [...]string{
	"cannonshell",
	"plasmasm",
	"plasmamd",
	"ultrashell",
	"plasmasm",
}

// projectileLifetimeSequence is render type 5's one fixed sequence
// [06 R-WFX-01 §1].
const projectileLifetimeSequence = "flamestream"

// ProjectileVisibilityModeBytes selects the published byte coverage
// representation: the current local-player grid. Mode zero selects the local
// bit in the one-point word grid instead [03 §5.4].
const ProjectileVisibilityModeBytes = 1

// ProjectileVisible evaluates exactly one gate for one projectile. The same
// sheared cell is used for either representation; no rendertype branch may
// call this again [03 §5.4].
func ProjectileVisible(v frame.VisibilityView, p frame.ProjectileView, mode uint8, localPlayer uint8) bool {
	return PointVisible(v, p.X, p.Y, p.Z, mode, localPlayer)
}

// PointVisible is the one-point coverage gate shared by every world-space
// presentation pixel: the projected tile is the world X and the world Z less
// half the world height, both in thirty-two unit tiles [03 §3.2][03 §5.4].
func PointVisible(v frame.VisibilityView, x, y, z numeric.Fixed, mode uint8, localPlayer uint8) bool {
	if !v.Valid || v.W <= 0 || v.H <= 0 {
		return false
	}
	px := int32(int16(int64(x) >> 16))
	py := int32(int16(int64(y) >> 16))
	pz := int32(int16(int64(z) >> 16))
	u := px >> 5
	row := (pz - (py >> 1)) >> 5
	if u < 0 || row < 0 || u >= v.W || row >= v.H {
		return false
	}
	idx := int(row*v.W + u)
	if mode&ProjectileVisibilityModeBytes != 0 || v.CoverageBytes {
		if _, ok := visibilityGridSize(v.W, v.H, len(v.Visible)); !ok {
			return false
		}
		return v.Visible[idx] != 0
	}
	if _, ok := visibilityGridSize(v.W, v.H, len(v.WordVisible)); !ok {
		return false
	}
	if localPlayer >= 10 {
		return false
	}
	return v.WordVisible[idx]&(uint16(1)<<localPlayer) != 0
}

// projectileDispatchOptions supplies only metadata established by the
// immutable publication boundary. The shared-GAF lookup reaches the fixed `fx`
// slots the retail trace closed and nothing else: a family whose art identity
// is not published stays suppressed rather than selecting a synthetic sprite or
// palette byte [06 R-WFX-01 §1].
func (c *Client) projectileDispatchOptions() render.ProjectileDispatchOptions {
	return render.ProjectileDispatchOptions{
		FrameCount: func(v frame.ProjectileView) (int, bool) {
			if v.FrameCount > 0 {
				return int(v.FrameCount), true
			}
			// The frame count of the two frame-selected families is a
			// property of the shared `fx` bank entry, not of the committed
			// record: type 4 wraps `(now − creationTick)` modulo the
			// selected sequence's length and type 5 scales its lifetime by
			// `flamestream`'s [06 R-WFX-01 §4]. The publication boundary
			// carries the authored selector; the entry it names is
			// presentation asset data and is resolved here, through the same
			// bank cache the frame blit goes through, so the modulus and the
			// frame that is blitted can never disagree.
			return c.projectileSequenceFrameCount(v)
		},
		Color: func(v frame.ProjectileView) (int32, int32, bool) {
			if !v.HasPrimaryColor {
				return 0, 0, false
			}
			var secondary int32
			if v.HasSecondaryColor {
				secondary = int32(v.SecondaryColor)
			}
			return int32(v.PrimaryColor), secondary, true
		},
		// GAF frames resolve only the fixed shared fx.gaf slots closed by the
		// retail trace. There is intentionally no AssetID or model-name fallback.
		SegmentPoints: func(v frame.ProjectileView) (first, second []render.ProjectilePoint, ok bool) {
			if c == nil || c.crt == nil {
				return nil, nil, false
			}
			// The two passes consume the client's PRIVATE presentation CRT copy
			// in admission order; the authoritative session stream is never
			// drawn here (DET-01). No straight-line substitute is emitted when
			// the copy is absent [03 §5.4][I4].
			return render.SnapshotSegmentedPointPasses(v, c.crt)
		},
		ResolveGAF: c.resolveProjectileGAF,
	}
}

// projectileSequenceFrameCount reports how many frames the shared `fx`
// sequence a frame-selected projectile draws holds [06 R-WFX-01 §1][06
// R-WFX-01 §4].
//
// Only the two families that index a sequence by frame have one: render type 4
// selects one of the five fixed slots with the weapon's `color` byte, and
// render type 5 has the single fixed `flamestream`. Every other family draws a
// model, a stroke or a single fixed frame and has no modulus to take, so it
// reports no count rather than a plausible one.
func (c *Client) projectileSequenceFrameCount(v frame.ProjectileView) (int, bool) {
	var name string
	switch v.RenderType {
	case render.RenderTypeSelectorGAF:
		if v.Selector < 0 || v.Selector >= int32(len(projectileSelectorSequences)) {
			return 0, false
		}
		name = projectileSelectorSequences[v.Selector]
	case render.RenderTypeLifetimeGAF:
		name = projectileLifetimeSequence
	default:
		return 0, false
	}
	gaf := c.ensureProjectileGAF()
	if gaf == nil {
		return 0, false
	}
	entry, ok := gaf.Find(name)
	if !ok || entry == nil || entry.FrameCount == 0 || len(entry.Frames) == 0 {
		return 0, false
	}
	n := int(entry.FrameCount)
	if len(entry.Frames) < n {
		n = len(entry.Frames)
	}
	return n, true
}

// ensureProjectileGAF performs one lazy lookup of the shared projectile bank.
// A failed load is cached for this client, matching the existing feature/fog
// cache boundary and ensuring one missing bank cannot cause repeated VFS work
// during a render loop [03 §4.4][I6].
func (c *Client) ensureProjectileGAF() *formats.GAF {
	if c == nil || c.projectileGAFLoaded {
		if c == nil {
			return nil
		}
		return c.projectileGAF
	}
	c.projectileGAFLoaded = true
	if r := c.modelTextures; r != nil && r.previewAssets != nil {
		// Settings previews share one decoded bank per content set, as they
		// share feature banks (DESIGN_INTERFACE_HUD_INPUT §3.17).
		bank := r.previewAssets.featureBank(projectileGAFBank)
		c.projectileGAF, c.projectileGAFErr = bank.gaf, bank.err
		return c.projectileGAF
	}
	if c.modelFS == nil {
		return nil
	}
	gaf, err := formats.LoadGAFFile(c.modelFS, projectileGAFPath)
	if err != nil {
		c.projectileGAFErr = err
		return nil
	}
	c.projectileGAF = gaf
	return gaf
}

// resolveProjectileGAF resolves only exact shared fx.gaf entries. Empty or
// content-supplied identities do not select a fallback: the authoritative
// frame carries no published route for those cases [03 §5.4][I9].
func (c *Client) resolveProjectileGAF(req render.ProjectileGAFRequest) (*formats.GAFFrame, bool) {
	var name string
	switch {
	case req.Base:
		// Render types 1, 3, 4, and 6 all use frame 0 of the fixed shadow entry.
		if req.Family != render.RenderTypeBaseSpriteModel && req.Family != render.RenderTypeBaseModelDistinct && req.Family != render.RenderTypeSelectorGAF && req.Family != render.RenderTypeRecordOrientation {
			return nil, false
		}
		name = "shadow"
	case req.Family == render.RenderTypeSelectorGAF:
		if req.Sequence < 0 || req.Sequence >= int32(len(projectileSelectorSequences)) {
			return nil, false
		}
		name = projectileSelectorSequences[req.Sequence]
	case req.Family == render.RenderTypeLifetimeGAF:
		// Type 5 has one fixed sequence; its lifetime/frame arithmetic is
		// performed before this resolver is called [03 §5.4].
		if req.Sequence != 0 {
			return nil, false
		}
		name = projectileLifetimeSequence
	default:
		// Type 2's lens is a startup-built displacement frame rather than a
		// shared fx.gaf entry. Its pixel mechanics remain owned by doc 03.
		return nil, false
	}

	gaf := c.ensureProjectileGAF()
	if gaf == nil || strings.TrimSpace(name) == "" {
		return nil, false
	}
	entry, ok := gaf.Find(name)
	if !ok || entry == nil || entry.FrameCount == 0 || len(entry.Frames) == 0 {
		return nil, false
	}
	if req.Frame < 0 || req.Frame >= int(entry.FrameCount) || req.Frame >= len(entry.Frames) {
		return nil, false
	}
	frame := entry.Frames[req.Frame].Frame
	if frame == nil {
		return nil, false
	}
	return frame, true
}

// defaultEffectBank is the bank an effect event names when it publishes an
// entry with no bank of its own. The engine's own fixed effect-slot table is
// bound from `fx` at startup [06 R-WFX-01 §1], and the smoke-puff entry the
// strip families blit is one of its rows, so an entry published without a bank
// is by construction one of those.
const defaultEffectBank = content.DefaultEffectBank

// effectBankKey folds an authored bank name exactly as the simulation's timing
// table does (content.CanonicalKey: ASCII letters folded, TDF whitespace
// trimmed, other bytes literal), so a bank name resolves to the same file here
// as in content.SimArt and the client draws exactly the banks the pool times.
func effectBankKey(name string) string {
	key := content.CanonicalKey(name)
	if key == "" {
		key = defaultEffectBank
	}
	return key
}

// EffectBank resolves immutable entry timing and root geometry by name, loading
// `anims/<name>.gaf` on the first miss and retaining metadata for this client.
// Frame fields are pixel-free placeholders; effectFrame materializes a selected
// root through bounded caches (DESIGN_PRESENTATION_CLIENT "On-demand effect art")
// [06 R-WFX-01 §1]. The lookup is case-insensitive, as retail's scan of the
// loaded banks is. A bank that will not load is memoised as a nil entry so a
// broken name costs one VFS attempt, not one per frame.
//
// Retail treats a missing bank as fatal — a modal message box naming the
// constructed path, then exit. A presentation client cannot do that to a
// running battle, so an unresolvable bank simply draws nothing; the identity
// stays published and unresolved rather than substituting a stand-in [I9].
func (c *Client) EffectBank(name string) *formats.GAF {
	if c == nil {
		return nil
	}
	key := effectBankKey(name)
	// Serialize presentation cache fills so each bank costs one VFS attempt.
	if mu := c.artMu; mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	if c.effectBanks == nil {
		c.effectBanks = map[string]*formats.GAF{}
	}
	if bank, ok := c.effectBanks[key]; ok {
		return bank
	}
	var bank *formats.GAF
	if c.modelFS != nil {
		path := content.EffectBankPath(key)
		if loaded, err := c.effectSourceLocked(key); err == nil {
			bank = effectBankMetadata(loaded)
		} else {
			c.recordArtDiagnosticLocked(path, "", err.Error())
		}
	}
	c.effectBanks[key] = bank
	return bank
}

// effectEntry finds one entry by name in one bank, both resolved by authored
// identity [06 R-WFX-01 §1].
func (c *Client) effectEntry(bankName, entryName string) (*formats.GAFEntry, bool) {
	if c == nil || strings.TrimSpace(entryName) == "" {
		return nil, false
	}
	bank := c.EffectBank(bankName)
	if bank == nil {
		return nil, false
	}
	entry, ok := bank.Find(entryName)
	if !ok || entry == nil || len(entry.Frames) == 0 {
		c.recordArtDiagnostic(content.EffectBankPath(effectBankKey(bankName)), strings.ToLower(entryName), "missing or empty animation entry")
		return nil, false
	}
	return entry, true
}

// effectDrawOptions keeps LHT admission terrain-bounded and resolves an
// effect's authored art.
//
// Authored LHT row and radius remain unresolved until the effect producer
// publishes them, and DrawEffectViews still emits no fabricated effect: an
// event that publishes no entry name, or one whose bank or entry does not
// resolve, draws nothing at all.
func (c *Client) effectDrawOptions() EffectDrawOptions {
	if c.effectOptions.ResolveFrame != nil {
		return c.effectOptions
	}
	// A client built without New binds them per call.
	return EffectDrawOptions{TerrainCoverage: c.terrainScreenCoverage, ResolveFrame: c.resolveEffectFrame, BlastSize: c.resolveBlastSize}
}

// resolveEffectFrame resolves an effect view's published art identity to the
// frame its animation cursor is on [06 R-WFX-01 §1].
//
// The identity is a PAIR: the entry name in Graphic and the bank that holds it
// in AssetID. A weapon authors them as `explosionart` inside `explosiongaf`,
// and both keys are required — a half-authored pair leaves retail's holder
// null and draws nothing, which is reproduced by the producer publishing no
// entry at all. An entry published without a bank comes from the engine's own
// fixed effect-slot table, which is bound from `fx`.
//
// The frame index is the pool's animation cursor, already advanced against the
// authored holds compiled into content.SimArt. It is clamped
// into the entry rather than rejected, because a cursor that has run past the
// end belongs to a sequence the pool is about to retire.
func (c *Client) resolveEffectFrame(view frame.EffectView, frameIndex int32) (*formats.GAFFrame, bool) {
	return c.effectFrame(view.AssetID, view.Graphic, frameIndex)
}

// terrainScreenCoverage identifies pixels that map to the loaded terrain
// rectangle in the shell's rebased screen coordinates.  It is intentionally
// evaluated against immutable world geometry and does not inspect the current
// indexed framebuffer, so LHT cannot brighten unit/effect/HUD pixels by
// accident.  Tile-level authored coverage beyond map bounds is not published
// by Terrain and remains TODO rather than guessed.
//
// A record pixel names the map pixel the record step's floor inverse gives it
// (DESIGN_GPU_RENDERER §14.1): the pixel itself at 1x, and the one under its
// 2x2 block at the detail step, where the lit discs are recorded magnified.
func (c *Client) terrainScreenCoverage(x, y int) bool {
	if c == nil || c.terrain == nil || c.cam == nil || c.terrain.CellW <= 0 || c.terrain.CellH <= 0 {
		return false
	}
	s := c.viewScale()
	mapX := int64(s.Inverse(int32(x))) + int64(c.cam.X)
	mapZ := int64(s.Inverse(int32(y))) + int64(c.cam.Z)
	return mapX >= 0 && mapZ >= 0 && mapX < int64(c.terrain.CellW)*16 && mapZ < int64(c.terrain.CellH)*16
}

// EffectEntryFrameCount reports how many frames one effect entry holds, out of
// the same bank cache the draw pass resolves frames through.
//
// The strip families' use of this length — a smoke puff's own last frame
// [03 R-STRIP-01 §2] — is AUTHORITATIVE and reads content.SimArt, not this
// accessor; what remains here is presentation's copy of the same question, and
// a test asserts the two readings agree.
func (c *Client) EffectEntryFrameCount(bank, entry string) (int, bool) {
	e, ok := c.effectEntry(bank, entry)
	if !ok {
		return 0, false
	}
	return len(e.Frames), true
}

// SetEffectArtLimit caps the effect art this client draws: an entry with a
// frame wider or taller than px authored pixels draws nothing, lends no light
// and leaves no scorch. Zero, the default, draws every entry. It is a host
// presentation choice, never set for a battle: the Nanolathe screen's preview
// sets it so that a content pack whose explosion entries are sized for
// something else — TA: Escalation's shield bubbles, 760–1140 pixels, which
// its stock weapons' hits and deaths also name — cannot cover the preview
// and stall it (DESIGN_INTERFACE_HUD_INPUT §3.17). Nothing here touches
// simulation state [I6].
func (c *Client) SetEffectArtLimit(px int) {
	if c != nil {
		c.effectArtLimit = max(px, 0)
	}
}

// effectArtAdmitted reports whether an entry lies within the art limit.
func (c *Client) effectArtAdmitted(entry *formats.GAFEntry) bool {
	if c == nil || c.effectArtLimit <= 0 || entry == nil {
		return true
	}
	for _, ref := range entry.Frames {
		if f := ref.Frame; f != nil && (int(f.Width) > c.effectArtLimit || int(f.Height) > c.effectArtLimit) {
			return false
		}
	}
	return true
}

// resolveBlastSize measures immutable authored art, not the tiny opening frame
// of a growing fireball. This modern-only size cache is retired with the bank.
func (c *Client) resolveBlastSize(view frame.EffectView) float32 {
	entry, ok := c.effectEntry(view.AssetID, view.Graphic)
	if !ok || !c.effectArtAdmitted(entry) {
		return 0
	}
	if size, ok := c.blastSizes[entry]; ok {
		return size
	}
	var size uint16
	for _, ref := range entry.Frames {
		if f := ref.Frame; f != nil {
			size = max(size, f.Width, f.Height)
		}
	}
	if c.blastSizes == nil {
		c.blastSizes = make(map[*formats.GAFEntry]float32)
	}
	c.blastSizes[entry] = float32(size)
	return float32(size)
}
