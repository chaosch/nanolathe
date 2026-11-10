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

//go:embed assets/*.png assets/*.json assets/OFL.txt assets/README.md
var fontAssets embed.FS

func init() {
	startCJKFetch = func() { cjkReady.Store(true) }
}

// 記錄當前正在加載的目標語言代碼
var loadingLangCode string

// EnableCJKForLanguage 由外部調用，明確指定要加載哪個語言的字體
func EnableCJKForLanguage(code string) {
	loadingLangCode = code
	EnableCJK() // 呼叫原本的加載邏輯
}

// selectLocalesFolder 優先讀取正在加載的目標語言，解決異步切換的時間差
func selectLocalesFolder() string {
	targetCode := loadingLangCode
	if targetCode == "" {
		targetCode = i18n.Language()
	}
	fmt.Printf("[film] 查詢字體 Folder: 目標 code='%s'\n", targetCode)

	for _, l := range i18n.Languages {
		if l.Code == targetCode {
			fmt.Printf("[film] 找到語言 %s, Folder='%s'\n", l.Code, l.Folder)
			return l.Folder
		}
	}
	fmt.Printf("[film] 警告：在 Languages 列表中找不到 code='%s'\n", targetCode)
	return ""
}

// readAsset 外部優先，內部兜底
func readAsset(name string) ([]byte, error) {
	folder := selectLocalesFolder()
	var data []byte
	var err error

	if folder == "" {
		// 沒有外部資料夾，讀取內置字體
		data, err = fontAssets.ReadFile("assets/" + name)
		if err == nil {
			fmt.Printf("[film] 載入內置字體 (embedded): %s\n", name)
		}
	} else {
		// 有外部資料夾，優先讀取外部字體
		extPath := filepath.Join(folder, name)
		data, err = os.ReadFile(extPath)
		if err == nil {
			fmt.Printf("[film] 載入外部字體 (external): %s\n", extPath)
		} else {
			// 外部讀取失敗，回退到內置
			fmt.Printf("[film] 外部字體讀取失敗 (%v)，回退載入內置字體: %s\n", err, name)
			data, err = fontAssets.ReadFile("assets/" + name)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("film: asset %q not found: %w", name, err)
	}
	return data, nil
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !errors.Is(err, os.ErrNotExist)
}
