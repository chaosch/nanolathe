//go:build !js

package film

import (
	"embed"
	"fmt"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"os"
	"path/filepath"
)

// These are presentation assets, independent of the retail game's fonts.
// Provenance, reproduction instructions and OFL terms accompany the atlases.
// The executable keeps both the base pair and the Chinese additions: the latter
// cost nothing until a language asks for them, which is what keeps nanolathe.exe
// a single file.
//
//go:embed assets/*.png assets/*.json assets/OFL.txt assets/README.md
var fontAssets embed.FS

func init() {
	// An embedded build always has the Chinese atlases at hand, so asking for
	// them is all it takes.
	startCJKFetch = func() { cjkReady.Store(true) }
}

//func readAsset(name string) ([]byte, error) {
//	return fontAssets.ReadFile("assets/" + name)
//}

var externalAssetsDir string

// SetAssetsDir 设置外部字体图集的基础路径
func SetAssetsDir(dir string) {
	externalAssetsDir = dir
}

// readAsset 外部优先，内部兜底
func readAsset(name string) ([]byte, error) {
	// 1. 优先尝试读取 locales/chinese/ 下同名的文件
	// 比如：~/.local/share/nanolathe/locales/chinese/chinese-small-cjk.png
	if externalAssetsDir != "" && i18n.Language() != "english" {
		// 这里假设 externalAssetsDir 传进来的是 ~/.local/share/nanolathe/locales
		// 拼上 chinese/name 即可
		extPath := filepath.Join(externalAssetsDir, i18n.Language(), name)
		if data, err := os.ReadFile(extPath); err == nil {
			return data, nil
		}
	}
	// 2. 回退到内嵌资源（保证没有外部文件时游戏能正常跑）
	data, err := fontAssets.ReadFile("assets/" + name)
	fmt.Printf("readAssets%s\n", data)
	if err != nil {
		return nil, fmt.Errorf("film: asset %q not found in external or internal: %w", name, err)
	}
	return data, nil
}
