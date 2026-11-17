#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Nanolathe CJK Font Atlas Generator
Nanolathe CJK 字体图集生成工具
【修改说明】
1. 保持原有光效、描边、渐变全部逻辑不动
2. 绘制字符使用墨水bbox尺寸画布，防止点阵TTF被freetype重采样糊掉
3. 输出格式变更：固定cell格子，glyph left=0 top=0，透明留白放在png内部
4. 半角(ASCII) cellW=em/2；全角 cellW=em；cellH=em
"""
import json
import math
import os
import sys
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont, ImageFilter, ImageChops

# ==========================================
# 路径设置
# ==========================================
# 强制读取和输出都在脚本所在的 tools 目录
SCRIPT_DIR = Path(__file__).resolve().parent
OUTPUT_DIR = SCRIPT_DIR
# ==========================================
# CONFIG 区域 (请根据你的语言包修改)
# ==========================================
# 直接在 tools 目录下找翻译文件
DICTIONARY_FILE = "chinese.json"
# 字体文件路径定义 (请放在 tools 目录下)
FONT_PIXEL_12 = "ZLabsPixel_12px_M_CN.ttf"
FONT_BUTTON_16 = "ChangBanDianSong-16-SC.ttf"
FONT_HD = "NotoSansSC.ttf"
FACES = {
    # === 战斗界面文字 (In‑Game HUD) ===
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
    # "body": {
    #     "em": 125, "weight": 400, "variable": True, "mode": "brush",
    #     "border": 0, "color": (220, 220, 220), "scale": 1.964, "fontFile": FONT_HD
    # },
    # "display": {
    #     "em": 121, "weight": 800, "variable": True, "mode": "brush",
    #     "border": 0, "color": (255, 255, 255), "scale": 1.964, "fontFile": FONT_HD
    # },
}
# ==========================================
# 内部常量
# ==========================================
WIDTH = 2048
OUTLINE_COLOR = (0, 0, 0)
SHADOW_STRENGTH = 160
SHADOW_START = 0.4

def is_halfwidth(c):
    return 0x20 <= ord(c) <= 0x7E

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

    # cell定义：半角em/2，全角em，cell高度等于em
    CELL_H = target_em
    SPACING = 2
    rows, glyphs = [], {}
    x, y = 0, 4

    for char in chars:
        # 固定cell尺寸
        if is_halfwidth(char):
            cw = target_em // 2
        else:
            cw = target_em
        ch = CELL_H

        # 获取墨水bbox(原版ls锚点，完全沿用原版度量)
        left, top, right, bottom = font.getbbox(char, anchor="ls")
        ink_w = math.ceil(right - left)
        ink_h = math.ceil(bottom - top)
        if ink_w <= 0 or ink_h <= 0:
            continue

        # ✅关键：用墨水大小画布渲染字符，避免大cell画布触发freetype重采样糊点阵字体
        if cfg["mode"] == "mask":
            raw_glyph = render_mask_glyph(char, font, ink_w, ink_h)
            pad = 0
        else:
            raw_glyph = render_styled_glyph(char, font, ink_w, ink_h, cfg["border"], cfg["mode"], cfg["color"], left, top)
            pad = max(2, cfg["border"])

        # 计算墨水在cell内部居中偏移（原版ls度量）
        ink_abs_top = top
        ink_abs_bottom = bottom
        ink_center_y = (ink_abs_top + ink_abs_bottom) / 2.0
        cell_center_y = CELL_H / 2.0
        off_y = cell_center_y - ink_center_y

        ink_center_x = left + (right-left)/2.0
        cell_center_x = cw / 2.0
        off_x = cell_center_x - ink_center_x

        # 中文标点额外向下1px，只移动图片，不改glyph元数据
        punc_down = 0
        if char in ("。", "，", "！", "？", "；", "：", "、"):
            punc_down = 1

        # 图集换行
        if x + cw + SPACING > WIDTH:
            x, y = 0, y + CELL_H + SPACING

        # ⭐输出json强制 left=0 top=0，w/h为完整cell格子，advance等于cell宽度
        glyphs[char] = dict(
            x=x, y=y,
            w=cw, h=ch,
            left=0, top=0,
            advance=cw,
            scale=scale
        )
        rows.append((char, x, y, cw, ch, raw_glyph, off_x, off_y + punc_down))
        x += cw + SPACING

    if not rows:
        return

    atlas_h = y + CELL_H + SPACING * 2
    atlas = Image.new("RGBA", (WIDTH, atlas_h), (0, 0, 0, 0))
    # 把渲染好的raw_glyph粘贴到cell画布，再贴进大图集
    for char, px, py, cw, ch, raw_glyph, off_x, off_y in rows:
        cell_canvas = Image.new("RGBA", (cw, ch), (0,0,0,0))
        cell_canvas.paste(raw_glyph, (int(off_x), int(off_y)), mask=raw_glyph)
        atlas.paste(cell_canvas, (px, py), cell_canvas)

    # cap等于cell高度，给Go渲染端使用
    out_png = OUTPUT_DIR / f"{name}-cjk.png"
    out_json = OUTPUT_DIR / f"{name}-cjk.json"
    atlas.save(out_png, optimize=True)
    meta_data = {"cap": CELL_H, "glyphs": glyphs}
    with open(out_json, "w", encoding="utf-8", newline="\n") as meta:
        meta.write(json.dumps(meta_data, separators=(",", ":"), ensure_ascii=False) + "\n")
    print(f"Generated: {out_png.name} ({WIDTH}x{atlas_h}) cap={CELL_H}")

if __name__ == "__main__":
    print("========================================")
    print(" Nanolathe CJK Font Atlas Generator")
    print("========================================")
    dict_path = OUTPUT_DIR / DICTIONARY_FILE
    chars = get_used_chars(dict_path)
    print(f"Found {len(chars)} unique characters to render.")
    for face_name, cfg in FACES.items():
        build(chars, name=face_name, cfg=cfg)
    print("Done!")
