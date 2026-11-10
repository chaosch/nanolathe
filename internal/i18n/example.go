package i18n

import (
	"fmt"
	"os"
	"path/filepath"
)

// createExampleLocale 建立一個範例語言包資料夾，並附帶字體生成工具
func createExampleLocale(localesDir string) {
	exampleDir := filepath.Join(localesDir, "example")
	toolsDir := filepath.Join(exampleDir, "tools")

	// 1. 建立目錄，如果報錯就印出來，但不中斷程式
	if err := os.MkdirAll(toolsDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "nanolathe: warning: cannot create example directory %q: %v\n", exampleDir, err)
	}

	// 2. 輔助寫入函數
	writeFile := func(filename, content string) {
		path := filepath.Join(exampleDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "nanolathe: warning: cannot write example file %q: %v\n", path, err)
		}
	}
	writeToolFile := func(filename, content string) {
		path := filepath.Join(toolsDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "nanolathe: warning: cannot write example tool %q: %v\n", path, err)
		}
	}

	// 3. 寫入 language.json
	writeFile("language.json", `{
  "name": "示例语言",
  "englishName": "Example Language",
  "cjk": true
}`)

	// 4. 寫入 example.json (翻譯字典)
	writeFile("example.json", `{
  "MOVE": "移动",
  "STOP": "停止",
  "ATTACK": "攻击",
  "The interface as written": "原版界面"
}`)

	// 5. 寫入 README.md (中英雙語)
	readme := `# Nanolathe 外部語言包開發指南 / External Language Pack Guide

## 中文說明

本資料夾是一個語言包的範例。如果你想為 Nanolathe 新增或修改語言，請按照以下步驟操作：

1. **複製資料夾**：將整個 ` + "`example`" + ` 資料夾複製一份，並將其重新命名為你的語言代碼（如 ` + "`zh_tw`" + ` 代表繁體中文，` + "`japanese`" + ` 代表日文）。
2. **修改 ` + "`language.json`" + `**：
   - ` + "`name`" + `：該語言自己的名字（如 "繁體中文"）。
   - ` + "`englishName`" + `：該語言的英文名字（如 "Traditional Chinese"）。
   - ` + "`cjk`" + `：如果是中日韓等全形字體語言，請設為 ` + "`true`" + `；如果是拼音語言設為 ` + "`false`" + `。
3. **修改翻譯檔**：將資料夾內的 ` + "`example.json`" + ` 重新命名為與資料夾同名的 JSON（如 ` + "`zh_tw.json`" + `），並在裡面填寫翻譯（格式為 ` + "`\"英文原文\": \"翻譯後的文字\"`" + `）。
4. **字體圖集生成**：
   - 進入 ` + "`tools`" + ` 資料夾，確保已安裝 Python 3 和 Pillow（` + "`pip install Pillow`" + `）。
   - 將你需要的字體檔（.ttf）放入 ` + "`tools`" + ` 資料夾（修改 ` + "`build_font.py`" + ` 中的 ` + "`FONT_*`" + ` 變數以匹配你的字體名）。
   - 執行 ` + "`python build_font.py`" + `，腳本會自動讀取上一層目錄的翻譯 JSON，並在語言包根目錄生成對應的 ` + "`*-cjk.png`" + ` 和 ` + "`*-cjk.json`" + ` 字體圖集。

## English Guide

This folder is an example of a language pack. If you want to add or modify a language for Nanolathe, follow these steps:

1. **Duplicate the folder**: Copy the entire ` + "`example`" + ` folder and rename it to your language code (e.g., ` + "`zh_tw`" + ` for Traditional Chinese, ` + "`japanese`" + ` for Japanese).
2. **Edit ` + "`language.json`" + `**:
   - ` + "`name`" + `: The name of the language in its own words (e.g., "繁體中文").
   - ` + "`englishName`" + `: The English name of the language (e.g., "Traditional Chinese").
   - ` + "`cjk`" + `: Set to ` + "`true`" + ` for CJK full-width languages, or ` + "`false`" + ` for phonetic alphabets.
3. **Edit the translation file**: Rename ` + "`example.json`" + ` inside the folder to match your language code (e.g., ` + "`zh_tw.json`" + `), and fill in the translations (format: ` + "`\"English text\": \"Translated text\"`" + `).
4. **Generate Font Atlas**:
   - Go to the ` + "`tools`" + ` folder. Ensure you have Python 3 and Pillow installed (` + "`pip install Pillow`" + `).
   - Place your font files (.ttf) into the ` + "`tools`" + ` folder (modify the ` + "`FONT_*`" + ` variables in ` + "`build_font.py`" + ` if your filenames differ).
   - Run ` + "`python build_font.py`" + `. The script will automatically read the JSON from the parent directory and generate the ` + "`*-cjk.png`" + ` and ` + "`*-cjk.json`" + ` atlases in the language pack root.
`
	writeFile("README.md", readme)

	// 6. 寫入 tools/build_font.py
	pyScript := `#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Nanolathe CJK Font Atlas Generator
Nanolathe CJK 字体图集生成工具
"""

import json
import math
import os
import sys
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont, ImageFilter, ImageChops

# ==========================================
# 路径设置 / Path Settings
# ==========================================
# 脚本所在的 tools 目录 / Script directory (tools)
SCRIPT_DIR = Path(__file__).resolve().parent        
# 语言包根目录 (如 .../locales/example 或 .../locales/zh_tw) / Language pack root directory
OUTPUT_DIR = SCRIPT_DIR.parent                      

# ==========================================
# CONFIG 区域 / Configuration Area
# ==========================================
# 【核心修改】：动态获取语言文件夹的名字作为 JSON 文件名
# Dynamically get the language folder's name as the JSON filename.
# 这样如果文件夹叫 example，它就读 example.json；叫 zh_tw 就读 zh_tw.json
DICTIONARY_FILE = f"{OUTPUT_DIR.name}.json"

# 字体文件路径定义 (请放在 tools 目录下)
# Font file paths (please place them in the tools directory)
FONT_PIXEL_12 = "ZLabsPixel_12px_M_CN.ttf"
FONT_BUTTON_16 = "ChangBanDianSong-16-SC.ttf"
FONT_HD = "NotoSansSC.ttf"

FACES = {
    # === 战斗界面文字 (In-Game HUD) ===
    "tiny": {
        "em": 12, "weight": 800, "variable": False, "mode": "mask",
        "border": 0, "color": None, "scale": 1, "fontFile": FONT_PIXEL_12
    },
    "small": {
        "em": 14, "weight": 800, "variable": False, "mode": "render",
        "border": 1, "color": (195, 195, 155), "scale": 1, "fontFile": FONT_BUTTON_16
    },
    "normal": {
        "em": 16, "weight": 800, "variable": False, "mode": "render",
        "border": 1, "color": (195, 195, 155), "scale": 1, "fontFile": FONT_BUTTON_16
    },

    # === Nanolathe 现代设置界面文字 (Modern UI) ===
    "body": {
        "em": 125, "weight": 400, "variable": True, "mode": "brush",
        "border": 0, "color": (220, 220, 220), "scale": 1.964, "fontFile": FONT_HD
    },
    "display": {
        "em": 121, "weight": 800, "variable": True, "mode": "brush",
        "border": 0, "color": (255, 255, 255), "scale": 1.964, "fontFile": FONT_HD
    },
}

# ==========================================
# 内部常量 / Internal Constants
# ==========================================
WIDTH = 2048
OUTLINE_COLOR = (0, 0, 0)
SHADOW_STRENGTH = 160
SHADOW_START = 0.4
CAP = 180


def get_used_chars(json_path):
    chars = set()
    for code in range(0x20, 0x7F):
        chars.add(chr(code))

    if not json_path.exists():
        print(f"[ERROR] Dictionary file not found: {json_path}")
        return sorted(chars)

    try:
        with open(json_path, 'r', encoding='utf-8') as f:
            data = json.load(f)
            for text in data.values():
                chars.update(text)
    except Exception as e:
        print(f"[ERROR] Failed to read dictionary: {e}")
        sys.exit(1)

    return sorted(c for c in chars if ord(c) >= 0x20 and c not in "{}")


def load_face(source, weight, size, variable=False):
    font_path = SCRIPT_DIR / source
    if not font_path.exists():
        print(f"[ERROR] Font file not found: {font_path}")
        sys.exit(1)

    font = ImageFont.truetype(str(font_path), size)

    if variable:
        try:
            axes = font.get_variation_axes()
            if axes and "wght" in [a.get("tag") for a in axes]:
                values = [weight if a.get("tag") == "wght" else a.get("default", 0) for a in axes]
                font.set_variation_by_axes(values)
        except Exception as e:
            pass

    return font


def render_mask_glyph(char, font, w, h):
    cell_w, cell_h = w, h
    base = Image.new('L', (cell_w, cell_h), 0)
    ImageDraw.Draw(base).text((0, 0), char, font=font, fill=255, anchor="ls")
    base = base.point(lambda p: 255 if p > 128 else 0)

    res = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))
    px_res, px_base = res.load(), base.load()
    for y in range(cell_h):
        for x in range(cell_w):
            if px_base[x, y] == 255:
                px_res[x, y] = (255, 255, 255, 255)
    return res


def render_styled_glyph(char, font, w, h, border, mode, color, left, top):
    pad = max(2, border)
    cell_w, cell_h = w + pad * 2, h + pad * 2

    draw_x = pad - left
    draw_y = pad - top

    base = Image.new('L', (cell_w, cell_h), 0)
    ImageDraw.Draw(base).text((draw_x, draw_y), char, font=font, fill=255, anchor="ls")
    base = base.point(lambda p: 255 if p > 128 else 0)
    res = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))

    if border > 0:
        filter_size = border * 2 + 1
        outline_mask = base.filter(ImageFilter.MaxFilter(filter_size))
        edge_layer = Image.new('RGBA', (cell_w, cell_h), (*OUTLINE_COLOR, 255))
        res.paste(edge_layer, (0, 0), outline_mask)

    if mode == "brush":
        color_layer = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))
        px_color, px_base = color_layer.load(), base.load()
        use_color = color if color else (255, 255, 255)
        for y in range(cell_h):
            for x in range(cell_w):
                if px_base[x, y] == 255:
                    px_color[x, y] = (*use_color, 255)
        res.alpha_composite(color_layer)
        return res

    if mode == "render":
        grad = Image.new('L', (cell_w, cell_h), 0)
        center_y, max_dist_y = cell_h / 2.0, cell_h / 2.0
        if max_dist_y == 0: max_dist_y = 1

        px_grad = grad.load()
        for y in range(cell_h):
            norm_dist = abs(y - center_y) / max_dist_y
            shadow_ratio = 0.0 if norm_dist <= SHADOW_START else (norm_dist - SHADOW_START) / (1.0 - SHADOW_START)
            val = max(0, min(255, 255 - int(shadow_ratio * SHADOW_STRENGTH)))
            for x in range(cell_w):
                px_grad[x, y] = val

        grad = grad.filter(ImageFilter.GaussianBlur(1.2))
        base_color = color if color else (195, 195, 155)

        color_layer = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))
        px_color, px_grad, px_base = color_layer.load(), grad.load(), base.load()

        for y in range(cell_h):
            for x in range(cell_w):
                if px_base[x, y] == 255:
                    brightness = px_grad[x, y] / 255.0
                    px_color[x, y] = (int(base_color[0] * brightness),
                                      int(base_color[1] * brightness),
                                      int(base_color[2] * brightness), 255)

        res.alpha_composite(color_layer)
        return res

    return res


def build(chars, name, cfg):
    target_em = max(6, cfg["em"])
    scale = cfg.get("scale", 1.0)

    font = load_face(cfg["fontFile"], cfg["weight"], target_em, cfg.get("variable", False))

    print(f"[{name}] Generating {len(chars)} glyphs with '{cfg['fontFile']}' (em={target_em}, scale={scale})")

    max_top = 0
    max_bottom = 0
    for char in chars:
        left, top, right, bottom = font.getbbox(char, anchor="ls")
        if top < max_top: max_top = top
        if bottom > max_bottom: max_bottom = bottom

    cell_height = max_bottom - max_top

    rows, glyphs = [], {}
    x, y, row_height, SPACING = 0, 4, 0, 6

    for char in chars:
        left, top, right, bottom = font.getbbox(char, anchor="ls")
        width = right - left
        if width <= 0:
            continue

        if x + width + SPACING * 2 > WIDTH:
            x, y, row_height = 0, y + row_height + SPACING, 0

        glyphs[char] = dict(
            x=x, y=y, w=width, h=cell_height,
            left=round(left * scale),
            top=round(max_top * scale),
            advance=font.getlength(char) * scale,
            scale=scale
        )

        rows.append((char, x, y, width, cell_height, left, top))
        x += width + SPACING
        row_height = max(row_height, cell_height)

    if not rows:
        return

    atlas_h = y + row_height + SPACING * 2
    atlas = Image.new("RGBA", (WIDTH, atlas_h), (0, 0, 0, 0))

    for char, px, py, w, h, left, top in rows:
        if cfg["mode"] == "mask":
            char_img = render_mask_glyph(char, font, w, h)
            pad = 0
        else:
            char_img = render_styled_glyph(char, font, w, h, cfg["border"], cfg["mode"], cfg["color"], left, max_top)
            pad = max(2, cfg["border"])

        atlas.paste(char_img, (px - pad, py - pad), char_img)

    out_png = OUTPUT_DIR / f"{name}-cjk.png"
    out_json = OUTPUT_DIR / f"{name}-cjk.json"

    atlas.save(out_png, optimize=True)
    with open(out_json, "w", encoding="utf-8", newline="\n") as meta:
        meta.write(json.dumps({"cap": CAP, "glyphs": glyphs}, separators=(",", ":"), ensure_ascii=False) + "\n")

    print(f"Generated: {out_png.name} ({WIDTH}x{atlas_h})")


if __name__ == "__main__":
    print("========================================")
    print(" Nanolathe CJK Font Atlas Generator")
    print("========================================")

    dict_path = OUTPUT_DIR / DICTIONARY_FILE
    print(f"Looking for dictionary: {dict_path}")
    chars = get_used_chars(dict_path)
    print(f"Found {len(chars)} unique characters to render.")

    for face_name, cfg in FACES.items():
        build(chars, face_name, cfg)

    print("Done!")
`
	writeToolFile("build_font.py", pyScript)
}
