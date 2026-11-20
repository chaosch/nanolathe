#!/usr/bin/env python3
import hashlib
import math
import os
import json
import sys
import unicodedata
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont, ImageFilter, ImageChops

# ==========================================
# 1. 强制写死的配置 (输入输出)
# ==========================================
SOURCE_FONT = r"zitidi.com.ttf"
Pixel_Font_12 =r"ZLabsPixel_12px_M_CN.ttf"
Pixel_Font_14 = r"ChangBanDianSong-16-SC.ttf"
PIXEL_FONT_16 = r"ChangBanDianSong-16-SC.ttf"

SCRIPT_DIR = Path(__file__).resolve().parent
INPUT_DIR = SCRIPT_DIR / "input"
OUT = SCRIPT_DIR / "output"

LANGUAGE = "chinese"
WIDTH = 2048
TARGET_COLOR = (195, 195, 155)
OUTLINE_COLOR = (0, 0, 0)

WEN_QUAN12=r"WenQuanYi.Bitmap.Song.12px.ttf"
WEN_QUAN13=r"WenQuanYi.Bitmap.Song.13px.ttf"
WEN_QUAN14=r"WenQuanYi.Bitmap.Song.14px.ttf"
WEN_QUAN15=r"WenQuanYi.Bitmap.Song.14px.ttf"
WEN_QUAN16=r"WenQuanYi.Bitmap.Song.16px.ttf"

OLD_FACE = {}
NEW_FACE = {
    "tiny":   {"em": 12, "weight": 800, "scale": 1, "fontFile": Pixel_Font_12, "variable": False, "mode": "mask",   "border": 0, "color": None},
    "small":  {"em": 14, "weight": 800, "scale": 1, "fontFile": Pixel_Font_14, "variable": False, "mode": "render", "border": 1, "color": TARGET_COLOR},
    "normal": {"em": 16, "weight": 800, "scale": 1, "fontFile": PIXEL_FONT_16, "variable": False, "mode": "render",  "border": 1, "color": TARGET_COLOR},
}
ALL_FACE = {}
ALL_FACE.update(OLD_FACE)
ALL_FACE.update(NEW_FACE)

EXTRA = "简体中文界面ABCDEFGHIJKLMNOPQRSTUVWXYabcdefghijklmnopqrstuvwxyz0123456789"

PALETTE = [
    (251, 251, 215), (229, 223, 189), (195, 195, 155), (171, 171, 131),
    (147, 147, 111), (119, 119, 87), (99, 99, 67), (83, 83, 51),
    (67, 67, 35), (51, 51, 23), (35, 35, 15), (27, 27, 7),
    (23, 23, 7), (19, 19, 0), (15, 15, 0), (11, 11, 0)
]

# ==========================================
# 全局调节参数
# ==========================================
DARKEN = 1
SHADOW_STRENGTH = 160
SHADOW_START = 0.3
BASELINE_RAISE = 1

def wanted():
    chars = set(EXTRA)
    for code in range(0x20, 0x7F):
        chars.add(chr(code))

    file_list = []
    for pat in ["*.txt", "*.json", "*.tdf"]:
        file_list.extend(INPUT_DIR.glob(pat))

    if not file_list:
        print(f"[WARN] No files matched in {INPUT_DIR}")
    else:
        for fp in file_list:
            print(f"[READ] {fp.name}")
            try:
                if fp.suffix.lower() == ".json":
                    data = json.loads(fp.read_text(encoding="utf-8"))
                    for text in data.values():
                        chars.update(text)
                else:
                    raw = fp.read_text(encoding="utf-8")
                    chars.update(raw)
            except Exception as e:
                print(f"[ERROR] Failed to read {fp.name}: {e}")

    return sorted(c for c in chars if ord(c) >= 0x20 and c not in "{}")

def load_face(source, weight, size, variable=False):
    font = ImageFont.truetype(source, size)
    if not variable:
        return font
    try:
        axes = font.get_variation_axes()
        if not axes:
            print(f"[WARN] {os.path.basename(source)} 标记为 variable 但未发现设计轴，忽略 weight={weight}")
            return font
        axis_tags = [a.get("tag") for a in axes]
        if "wght" in axis_tags:
            values = []
            for a in axes:
                if a.get("tag") == "wght":
                    values.append(weight)
                else:
                    values.append(a.get("default", a.get("min", 0)))
            font.set_variation_by_axes(values)
        else:
            print(f"[WARN] {os.path.basename(source)} 无可用的 wght 轴，忽略 weight={weight}")
    except Exception as e:
        print(f"[WARN] 设置可变轴失败 {os.path.basename(source)}: {e}")
    return font

def render_mask_glyph(char, font, w, h):
    cell_w = w
    cell_h = h

    base = Image.new('L', (cell_w, cell_h), 0)
    d = ImageDraw.Draw(base)
    d.text((0, 0), char, font=font, fill=255, anchor="lt")

    base = base.point(lambda p: 255 if p > 128 else 0)

    res = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))
    px_res = res.load()
    px_base = base.load()
    for y in range(cell_h):
        for x in range(cell_w):
            if px_base[x, y] == 255:
                px_res[x, y] = (255, 255, 255, 255)
    return res

def render_styled_glyph1(char, font, w, h, border, mode, color):
    pad = max(1, border) if border > 0 else 2
    cell_w = w + pad * 2
    cell_h = h + pad * 2

    base = Image.new('L', (cell_w, cell_h), 0)
    d = ImageDraw.Draw(base)
    d.text((pad, pad), char, font=font, fill=255, anchor="lt")

    base = base.point(lambda p: 255 if p > 128 else 0)

    res = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))

    if border > 0:
        outline = Image.new('L', (cell_w, cell_h), 0)
        px_out = outline.load()
        px_base = base.load()
        dirs = [(-1, 0), (1, 0), (0, -1), (0, 1)]
        for y in range(cell_h):
            for x in range(cell_w):
                if px_base[x, y] == 255:
                    for dx, dy in dirs:
                        nx = x + dx
                        ny = y + dy
                        if 0 <= nx < cell_w and 0 <= ny < cell_h:
                            px_out[nx, ny] = 255
        edge_layer = Image.new('RGBA', (cell_w, cell_h), (*OUTLINE_COLOR, 255))
        res.paste(edge_layer, (0, 0), outline)

    if mode == "brush":
        color_layer = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))
        px_color = color_layer.load()
        px_base = base.load()
        use_color = color if color else (255, 255, 255)

        for y in range(cell_h):
            for x in range(cell_w):
                if px_base[x, y] == 255:
                    px_color[x, y] = (*use_color, 255)

        res.alpha_composite(color_layer)
        return res

    if mode == "render":
        grad = Image.new('L', (cell_w, cell_h), 0)
        center_y = cell_h / 2.0
        max_dist_y = cell_h / 2.0
        if max_dist_y == 0: max_dist_y = 1

        px_grad = grad.load()
        for y in range(cell_h):
            dist = abs(y - center_y)
            norm_dist = dist / max_dist_y

            if norm_dist <= SHADOW_START:
                shadow_ratio = 0.0
            else:
                shadow_ratio = (norm_dist - SHADOW_START) / (1.0 - SHADOW_START)

            val = 255 - int(shadow_ratio * SHADOW_STRENGTH)
            val = max(0, min(255, val))

            for x in range(cell_w):
                px_grad[x, y] = val

        blur = 1.2 if w <= 14 else 0.6
        grad = grad.filter(ImageFilter.GaussianBlur(1.2))
        base_color = color if color else PALETTE[0]

        color_layer = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))
        px_color = color_layer.load()
        px_grad = grad.load()
        px_base = base.load()

        for y in range(cell_h):
            for x in range(cell_w):
                if px_base[x, y] == 255:
                    brightness = px_grad[x, y] / 255.0
                    r = int(base_color[0] * brightness)
                    g = int(base_color[1] * brightness)
                    b = int(base_color[2] * brightness)
                    px_color[x, y] = (r, g, b, 255)

        res.alpha_composite(color_layer)
        return res

    return res

def render_styled_glyph(char, font, w, h, border, mode, color):
    pad = max(1, border) if border > 0 else 2
    cell_w = w + pad * 2
    cell_h = h + pad * 2

    base = Image.new('L', (cell_w, cell_h), 0)
    d = ImageDraw.Draw(base)
    d.text((pad, pad), char, font=font, fill=255, anchor="lt")

    base = base.point(lambda p: 255 if p > 128 else 0)

    res = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))

    if border > 0:
        outline = Image.new('L', (cell_w, cell_h), 0)
        px_out = outline.load()
        px_base = base.load()
        dirs = [(-1, 0), (1, 0), (0, -1), (0, 1)]
        for y in range(cell_h):
            for x in range(cell_w):
                if px_base[x, y] == 255:
                    for dx, dy in dirs:
                        nx = x + dx
                        ny = y + dy
                        if 0 <= nx < cell_w and 0 <= ny < cell_h:
                            px_out[nx, ny] = 255
        edge_layer = Image.new('RGBA', (cell_w, cell_h), (*OUTLINE_COLOR, 255))
        res.paste(edge_layer, (0, 0), outline)

    if mode == "brush":
        color_layer = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))
        px_color = color_layer.load()
        px_base = base.load()
        use_color = color if color else (255, 255, 255)

        for y in range(cell_h):
            for x in range(cell_w):
                if px_base[x, y] == 255:
                    px_color[x, y] = (*use_color, 255)

        res.alpha_composite(color_layer)
        return res

    if mode == "render":
        grad = Image.new('L', (cell_w, cell_h), 0)
        center_y = cell_h / 2.0
        max_dist_y = cell_h / 2.0
        if max_dist_y == 0: max_dist_y = 1

        px_grad = grad.load()
        for y in range(cell_h):
            dist = abs(y - center_y)
            norm_dist = dist / max_dist_y

            if norm_dist <= SHADOW_START:
                shadow_ratio = 0.0
            else:
                shadow_ratio = (norm_dist - SHADOW_START) / (1.0 - SHADOW_START)

            val = 255 - int(shadow_ratio * SHADOW_STRENGTH)
            val = max(0, min(255, val))

            for x in range(cell_w):
                px_grad[x, y] = val

        blur = 1.2 if w <= 14 else 0.6
        grad = grad.filter(ImageFilter.GaussianBlur(blur))
        base_color = color if color else PALETTE[0]

        color_layer = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))
        px_color = color_layer.load()
        px_grad = grad.load()
        px_base = base.load()

        for y in range(cell_h):
            for x in range(cell_w):
                if px_base[x, y] == 255:
                    brightness = px_grad[x, y] / 255.0
                    r = int(base_color[0] * brightness)
                    g = int(base_color[1] * brightness)
                    b = int(base_color[2] * brightness)
                    px_color[x, y] = (r, g, b, 255)

        res.alpha_composite(color_layer)
        return res

    return res

def build(chars, name, target_em, weight, scale, fontFile, variable, mode, border, color):
    CAP = 180
    real_em = target_em if target_em >= 6 else 6
    font = load_face(fontFile, weight, real_em, variable)

    glyph_bbox_info = {}
    for char in chars:
        try:
            bbox = font.getbbox(char, anchor="ls")
            l, t, r, b = bbox
            if r - l <= 0 and char != " ":
                continue
            glyph_bbox_info[char] = (l, t, r, b)
        except Exception:
            continue

    max_ascent = 0
    max_descent = 0
    for char, (l, t, r, b) in glyph_bbox_info.items():
        max_ascent = max(max_ascent, abs(t))
        max_descent = max(max_descent, abs(b))

    cell_height = int(math.ceil(max_ascent + max_descent))
    cell_baseline = int(math.ceil(max_ascent))

    pad = max(2, border) if mode != "mask" else 0

    rows, glyphs = [], {}
    x, y = 0, 0
    row_height = cell_height + pad * 2
    SPACING = 2

    for char in chars:
        if char not in glyph_bbox_info:
            continue
        left, top, right, bottom = glyph_bbox_info[char]

        ink_width = right - left
        if ink_width <= 0:
            ink_width = int(math.ceil(font.getlength(char)))

        # ASCII字符收缩，CJK汉字保持原宽
        if ord(char) < 0x80:
            shrink = 0.82
            ink_width = ink_width * shrink

        # ==========================================
        # ★ 这里的 w 就是原生的墨迹宽度 + 左右描边（如果有）
        # 坚决不加任何透明留白，是几就是几
        # ==========================================
        width = int(math.ceil(ink_width)) + border * 2
        height = cell_height + border * 2


        if x + width + SPACING > WIDTH:
            x = 0
            y += row_height + SPACING

        # ==========================================
        # ★ 垂直位置逻辑（坚决不改）
        # ==========================================
        if name == "small":
            # small 的基线修复
            glyph_y_offset = cell_baseline - abs(top) + pad
        else:
            # tiny 和 normal 的完美原版逻辑，一点没动
            glyph_y_offset = cell_baseline - abs(top) + pad

        # ==========================================
        # ★ 绝对原则：advance 完美等于 w
        # ==========================================
        advance = width

        glyphs[char] = dict(
            x=x, y=y, w=width, h=height,
            left=0,
            top=0,
            advance=advance,
            scale=scale
        )

        rows.append((char, x, y, width, height, left, glyph_y_offset))
        x += width + SPACING

    # 3. 绘制 PNG（完全使用原始的不裁切排版）
    atlas_h = y + row_height + SPACING
    atlas = Image.new("RGBA", (WIDTH, atlas_h), (0, 0, 0, 0))

    for char, px, py, w, h, left, offset in rows:
        if mode == "mask":
            char_img = Image.new('RGBA', (w, h), (0, 0, 0, 0))
            draw = ImageDraw.Draw(char_img)
            target_x = -left + pad
            draw.text((target_x, offset), char, font=font, fill=(255, 255, 255, 255), anchor="lt")

            px_res = char_img.load()
            for j in range(h):
                for i in range(w):
                    if px_res[i, j][3] > 128:
                        px_res[i, j] = (255, 255, 255, 255)
                    else:
                        px_res[i, j] = (0, 0, 0, 0)
            atlas.paste(char_img, (px, py), char_img)
        else:
            # 直接把你准备好的尺寸传给渲染函数
            char_img = render_styled_glyph(char, font, w - border*2, h - border*2, border, mode, color)
            atlas.paste(char_img, (px, py), char_img)

    out_png = OUT / f"{name}-cjk.png"
    out_json = OUT / f"{name}-cjk.json"
    atlas.save(out_png, optimize=True)
    with open(out_json, "w", encoding="utf-8", newline="\n") as meta:
        meta.write(json.dumps(
            {"cap": CAP, "glyphs": glyphs},
            separators=(",", ":"), ensure_ascii=False
        ) + "\n")

    print(f"{name}-cjk: real_em={real_em}, cell_h={height} (with pad={pad}), out={out_png}")

if __name__ == "__main__":
    INPUT_DIR.mkdir(parents=True, exist_ok=True)
    OUT.mkdir(parents=True, exist_ok=True)
    chars = wanted()
    print(f"Found {len(chars)} unique characters to render.")
    for face_name, cfg in ALL_FACE.items():
        build(
            chars, face_name, cfg["em"], cfg["weight"], cfg["scale"],
            cfg["fontFile"], cfg.get("variable", False), cfg["mode"], cfg["border"], cfg["color"]
        )
    print("Done!")