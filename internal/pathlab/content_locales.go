package pathlab

import (
	"errors"
	"fmt"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"os"
	"path/filepath"
	"strings"
)

// localeRootForLocales returns the content root holding the translation
// overlay for language, or "" when the language has no installed overlay.
//
// A locale root is an ordinary content root: it may hold gamedata/translate.tdf
// as a loose file, or an HPI-family archive that contains it. MountGameDirectories
// handles both, so the root only has to exist and be a directory.
//
// Layout: <base>/locales/<language>/
//
//	zh/gamedata/translate.tdf      (loose)
//	zh/translation.ufo             (archived)
//
// An empty language, "en", "english" or "retail" selects no overlay: the base
// install's own translate.tdf is the retail behaviour and must not be shadowed
// by an identity mapping.
func localeRootForLocales() string {
	if !i18n.I18nLanguageNeedCJK() {
		return ""
	}
	language := i18n.Language()

	lang := strings.ToLower(strings.TrimSpace(language))
	if lang == "" || lang == "en" || lang == "english" || lang == "retail" {
		return ""
	}
	// 语言目录名只允许安全字符，避免拼出路径穿越。
	if strings.ContainsAny(lang, `/\`) || lang == "." || lang == ".." {
		return ""
	}
	root, _ := defaultLocalesDir()
	stat, err := os.Stat(root)
	if err != nil || !stat.IsDir() {
		return ""
	}
	return root
}

func defaultLocalesDir() (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "nanolathe", "locales"), nil
}

func createLocalesDir() (string, error) {
	if localesDir, r := defaultLocalesDir(); r == nil {
		if exist, err := DirExists(localesDir); exist {
			return localesDir, err
		}
		if err := os.MkdirAll(localesDir, 0755); err != nil {
			return "", err
		} else {
			return localesDir, nil
		}
	}
	return "", errors.New("could not create locales dir")
}

// DirExists 判断目录是否存在
func DirExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		// 如果错误是“文件或目录不存在”，返回 false, nil
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		// 其他错误（比如权限不足），返回 false 和具体错误
		return false, err
	}

	// 路径存在，但检查它是不是目录
	if !info.IsDir() {
		return false, fmt.Errorf("路径 %s 存在，但不是一个目录", path)
	}

	return true, nil
}

// If missing, fallback to embedded localization assets.

func verifyLocalesFolder(baseDir, folder string) bool {
	files := []string{"tiny-cjk.png", "normal-cjk.png", "small-cjk.json", "normal-cjk.json", "body-cjk.png", "tiny-cjk.json", "display-cjk.png", "small-cjk.png", "display.json", "display-cjk.json", "body-cjk.json", "display.png", "body.png", "body.json"}
	files = append(files, folder+".json")
	for _, file := range files {
		filename := filepath.Join(baseDir, folder, file)
		if info, err := os.Stat(filename); err != nil || info.IsDir() {
			return false
		}
	}
	return true
}
