package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

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
