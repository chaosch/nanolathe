package main

import (
	"image/color"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"github.com/nanolathe-gg/nanolathe/internal/platform/screenkit"
)

type CJKRenderReq struct {
	Text   string
	X, Y   float64
	Width  float64
	Height float64
	Align  int
}

var (
	produceCJK    []CJKRenderReq // 生产者队列：原生管线写入
	consumeCJK    []CJKRenderReq // 消费者队列：Overlay绘制
	globalCJKFont *screenkit.Font
	cjkLayer      *ebiten.Image
	DropCJKFrame  bool // ★ 原生代码调用这个开关，用来清空文字层、丢弃待绘制文字
)

func EnqueueCJKText(text string, x, y, width, height float64, align int) {
	produceCJK = append(produceCJK, CJKRenderReq{
		Text: text, X: x, Y: y, Width: width, Height: height, Align: align,
	})
}

// ClearCJKOverlayFromNative 【给原生游戏调用的导出函数】
// 原生切换菜单时直接调用这个，不需要碰图层、Ebitengine内部细节
func ClearCJKOverlayFromNative() {
	DropCJKFrame = true
}

func mapToScreen(screen *ebiten.Image) (scale, offX, offY float64) {
	winW := float64(screen.Bounds().Dx())
	winH := float64(screen.Bounds().Dy())
	scale = math.Min(winW/640.0, winH/480.0)
	offX = (winW - 640.0*scale) / 2
	offY = (winH - 480.0*scale) / 2
	return
}

func (g *gameShell) drawRetailFilmCJKCylindrical(c *client.Client, text string, x, baselineY, maxWidth int, controlHeight int, palColor byte, attribs uint32) bool {
	if i18n.Language() != "chinese" {
		return false
	}
	tr := i18n.T(text)
	if tr == text {
		return false
	}
	if c == nil || !film.CJKReady() {
		return false
	}

	if len(tr) >= 2 && tr[0] == '\\' {
		if tr[1] == 'd' || tr[1] == 'D' {
			tr = tr[2:]
		} else if tr[1] == 'b' || tr[1] == 'B' {
			tr = tr[2:]
		}
	}

	align := 0
	if attribs&2 != 0 {
		align = 1
	}
	if attribs&4 != 0 {
		align = 2
	}

	safeWidth := float64(maxWidth)
	if safeWidth <= 0 {
		safeWidth = 9999
	}

	EnqueueCJKText(tr, float64(x), float64(baselineY), safeWidth, float64(controlHeight), align)
	return true
}

type globalCJKOverlay struct{}

func (o *globalCJKOverlay) Active() bool  { return true }
func (o *globalCJKOverlay) Update() error { return nil }

func (o *globalCJKOverlay) Draw(screen *ebiten.Image, a, b, c float64) {
	if globalCJKFont == nil {
		globalCJKFont = screenkit.LoadFonts().Display
	}

	if cjkLayer == nil || cjkLayer.Bounds() != screen.Bounds() {
		cjkLayer = ebiten.NewImage(screen.Bounds().Dx(), screen.Bounds().Dy())
	}

	// ========== 屏障点：检测原生下发的清空指令 ==========
	if DropCJKFrame {
		produceCJK = produceCJK[:0] // 丢掉所有已经入队、还没绘制的文字
		cjkLayer.Clear()            // 清空当前文字图层画布
		DropCJKFrame = false        // 清除标记，只生效一次
		// 直接贴空图层返回，本帧不再绘制任何文字
		screen.DrawImage(cjkLayer, nil)
		return
	}

	// 正常流程：双缓冲交换快照
	consumeCJK, produceCJK = produceCJK, consumeCJK[:0]
	cjkLayer.Clear()

	if globalCJKFont == nil {
		screen.DrawImage(cjkLayer, nil)
		return
	}

	scale, offX, offY := mapToScreen(screen)
	for _, p := range consumeCJK {
		boxX := p.X*scale + offX
		boxY := p.Y*scale + offY
		boxW := p.Width * scale
		boxH := p.Height * scale

		if boxW <= 2 || boxH <= 2 {
			continue
		}

		const paddingV = 0.15
		realSize := boxH * (1.0 - paddingV*2)
		centerY := boxY + boxH/2.0
		realY := centerY + realSize/2.0 - realSize*0.08

		realX := boxX
		if p.Align == 1 {
			realX = boxX + boxW/2.0
		}
		if p.Align == 2 {
			realX = boxX + boxW
		}

		style := screenkit.Style{
			Size:     realSize,
			Top:      color.RGBA{251, 251, 215, 255},
			Bottom:   color.RGBA{67, 67, 35, 255},
			Align:    p.Align,
			Shadow:   0.1,
			Verbatim: true,
		}

		globalCJKFont.Draw(cjkLayer, p.Text, realX, realY, style)
	}

	consumeCJK = consumeCJK[:0]
	screen.DrawImage(cjkLayer, nil)
}
