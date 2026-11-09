//go:build !js

package film

import (
	"embed"
	"errors"
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
	folder := selectLocalesFolder()
	var data []byte
	var err error
	if folder == "" {
		data, err = fontAssets.ReadFile("assets/" + name)
		println("load  internal locales face from folder:", name, "assets")
	} else {
		data, err = os.ReadFile(filepath.Join(folder, name))
		println("load  external locales face from folder:%", filepath.Join(folder, name))
		if err != nil {
			data, err = fontAssets.ReadFile("assets/" + name)
			println("load  internal face  from folder fallback", name, "assets", "fallback")
		}
	}
	if err != nil {
		return nil, fmt.Errorf("film: asset %q not found in external or internal: %w", name, err)
	}
	return data, nil
}

func selectLocalesFolder() string {
	languages := i18n.Languages
	for _, l := range languages {
		if l.Code == i18n.Language() {
			return l.Folder
		}
	}
	return ""
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true // 存在
	}
	if errors.Is(err, os.ErrNotExist) {
		return false // 不存在
	}
	// 其他错误（比如权限不足），这里通常也视为不可用
	return false
}
