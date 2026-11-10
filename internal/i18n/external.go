package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func defaultLocalesDir() (string, bool) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", false
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "nanolathe", "locales"), true
}

func createLocalesDir() (string, bool) {
	if localesDir, r := defaultLocalesDir(); r {
		if exist, _ := DirExists(localesDir); exist {
			//createExampleLocale(localesDir)
			return localesDir, true
		}
		if err := os.MkdirAll(localesDir, 0755); err != nil {
			return localesDir, false
		}
		// 【新增】：順便建立範例目錄
		//createExampleLocale(localesDir)
		return localesDir, true
	}
	return "", false
}

func DirExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

// ScanExternalLocales 掃描外部語言資料夾，並更新/追加到 Languages 列表
func ScanExternalLocales() error {
	localesDir, ok := createLocalesDir()
	if !ok {
		return nil
	}

	entries, err := os.ReadDir(localesDir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		code := entry.Name()
		folderPath := filepath.Join(localesDir, code)

		// 必須有同名 JSON 翻譯文件
		if _, err := os.Stat(filepath.Join(folderPath, code+".json")); os.IsNotExist(err) {
			continue
		}

		// 默認值
		langName := code
		isCJK := LanguageNeedCJK(code)

		// 讀取 language.json 描述檔
		englishName := code // 預設用 code

		metaPath := filepath.Join(folderPath, "language.json")
		if data, err := os.ReadFile(metaPath); err == nil {
			var meta struct {
				Name        string `json:"name"`
				EnglishName string `json:"englishName"`
				CJK         *bool  `json:"cjk"`
			}
			if err := json.Unmarshal(data, &meta); err == nil {
				if meta.Name != "" {
					langName = meta.Name
				}
				if meta.EnglishName != "" {
					englishName = meta.EnglishName
				}
				if meta.CJK != nil {
					isCJK = *meta.CJK
				}
			}
		}

		// 查找是否已存在
		idx := -1
		for i, l := range Languages {
			if l.Code == code {
				idx = i
				break
			}
		}

		scaned := struct {
			Code, Name, Folder string
			IsCJK              bool
			EnglishName        string
		}{
			Code:        code,
			Name:        langName,
			Folder:      folderPath,
			IsCJK:       isCJK,
			EnglishName: englishName,
		}

		if idx == -1 {
			// 全新語言：完全採用外部定義
			Languages = append(Languages, scaned)
			fmt.Printf("[i18n] 發現新外部語言: %s (%s)\n", langName, code)
		} else {
			// 已存在的內建語言
			if code == "chinese" {
				// 只有簡中允許被外部覆蓋路徑（替換字體和翻譯）
				Languages[idx].Folder = folderPath
				fmt.Printf("[i18n] 覆蓋內置語言的路徑: %s\n", code)
			} else {
				// 英文、烏克蘭等純字符語言，完全忽略外部同名資料夾
				fmt.Printf("[i18n] 忽略外部同名資料夾 (禁止覆蓋): %s\n", code)
			}
		}
	}
	return nil
}
