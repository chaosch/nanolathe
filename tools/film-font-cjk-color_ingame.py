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
# 默认大字体源（用于 small/normal 等带光影的字）
SOURCE_FONT = r"..\..\NotoSansSC.ttf"

# ★ 新增：tiny 专用 16px 点阵字体
# PIXEL_FONT_16 = r"..\..\ZhengGeDianHei-16.ttf"
Pixel_Font_12 = r"..\..\ZLabsPixel_12px_M_CN.ttf"
PIXEL_FONT_16 = r"..\..\ZhengGeDianHei-16.ttf"

CATALOG_JSON = r"..\internal\i18n\locales\chinese.json"
OUT = Path(__file__).resolve().parent.parent / "internal/film/assets"
LANGUAGE = "chinese"
WIDTH = 2048

OLD_FACE = {}
NEW_FACE = {
    "tiny": {"em": 12, "weight": 800, "scale": 1,"fontFile": r"..\..\ZLabsPixel_12px_M_CN.ttf"},
    "small":    {"em": 16, "weight": 800, "scale": 1, "fontFile": r"..\..\ZhengGeDianHei-16.ttf"},
    #     "normal":   {"em": 28, "weight": 800, "scale": 1},
    #     "medium":   {"em": 36, "weight": 800, "scale": 1},
    #     "large":    {"em": 44, "weight": 800, "scale": 1},
    #     "title":    {"em": 52, "weight": 800, "scale": 1},
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
DARKEN = 0.85
PALETTE = [tuple(int(c * DARKEN) for c in color) for color in PALETTE]

OUTLINE_COLOR = (0, 0, 0)


def get_outline_width(em, is_tiny):
    if is_tiny:
        return 0
    if em <= 20:
        return 0
    elif em <= 30:
        return 1
    elif em <= 40:
        return 1
    elif em <= 50:
        return 1
    elif em <= 60:
        return 1
    else:
        return 7


def gray_to_palette(gray_val):
    if gray_val == 0:
        return (0, 0, 0)
    ratio = (gray_val / 255.0) ** 0.8
    idx = int((1.0 - ratio) * (len(PALETTE) - 1))
    idx = max(0, min(idx, len(PALETTE) - 1))
    return PALETTE[idx]


def wanted(catalogs):
    chars = set(EXTRA)
    for code in range(0x20, 0x7F):
        chars.add(chr(code))
    for path in catalogs:
        if not os.path.exists(path):
            continue
        for text in json.loads(Path(path).read_text(encoding="utf-8")).values():
            chars.update(text)
    return sorted(c for c in chars if ord(c) >= 0x20 and c not in "{}")


def load_face(source, weight, size):
    font = ImageFont.truetype(source, size)
    try:
        font.set_variation_by_axes([weight])
    except Exception:
        pass
    return font


def render_colored_glyph(char, font, w, h, outline_width, is_tiny=False):
    pad = 0 if is_tiny else max(2, outline_width)
    cell_w = w + pad * 2
    cell_h = h + pad * 2

    # 1. 提取字形遮罩
    base = Image.new('L', (cell_w, cell_h), 0)
    d = ImageDraw.Draw(base)
    d.text((pad, pad), char, font=font, fill=255, anchor="lt")

    res = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))

    # ★ 如果是 tiny：直接输出纯白像素，取消抗锯齿灰边，无描边
    if is_tiny:
        color_layer = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))
        px_res = color_layer.load()
        px_base = base.load()

        for y in range(cell_h):
            for x in range(cell_w):
                # 如果用 255,255,255 就是纯白；如果是其他 RGB，就填进去
                if px_base[x, y] > 128:
                    px_res[x, y] = (255, 255, 255, 255) # 把这里改成你想要的 RGB，例如 (250, 200, 50)

        res.alpha_composite(color_layer)
        return res

    # --- 以下是普通字号的光照和描边逻辑 ---
    outline = Image.new('L', (cell_w, cell_h), 0)
    do = ImageDraw.Draw(outline)
    r = outline_width
    for dx in range(-r, r + 1):
        for dy in range(-r, r + 1):
            if dx * dx + dy * dy > r * r:
                continue
            do.text((pad + dx, pad + dy), char, font=font, fill=255, anchor="lt")

    edge_layer = Image.new('RGBA', (cell_w, cell_h), (*OUTLINE_COLOR, 255))
    res.paste(edge_layer, (0, 0), outline)

    grad = Image.new('L', (cell_w, cell_h), 0)
    center_y = cell_h / 2.0
    max_dist_y = cell_h / 2.0
    if max_dist_y == 0: max_dist_y = 1
    for y in range(cell_h):
        dist = abs(y - center_y)
        norm_dist = dist / max_dist_y
        val = 255 - int(norm_dist * 160)
        val = max(0, min(255, val))
        for x in range(cell_w):
            grad.putpixel((x, y), val)

    grad = grad.filter(ImageFilter.GaussianBlur(1.2))
    base_color = PALETTE[0]
    color_layer = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))
    px_res = color_layer.load()
    px_base = base.load()
    px_grad = grad.load()

    for y in range(cell_h):
        for x in range(cell_w):
            if px_base[x, y] > 0:
                alpha = px_base[x, y]
                brightness = px_grad[x, y] / 255.0
                r = int(base_color[0] * brightness)
                g = int(base_color[1] * brightness)
                b = int(base_color[2] * brightness)
                px_res[x, y] = (r, g, b, alpha)

    res.alpha_composite(color_layer)
    return res


def build(chars, name, target_em, weight, scale, fontFile):
    CAP = 180
    is_tiny = (target_em < 20)

    # ★ 核心：按名字选择字体源和描边宽度
    if is_tiny:
        outline_width = 0
        real_em = target_em  # 强制 16px
    else:
        outline_width = get_outline_width(target_em, False)
        real_em = target_em - (outline_width * 2)

    font_path = fontFile
    if real_em < 6:
        real_em = 6

    font = load_face(font_path, weight, real_em)

    max_h = 0
    max_w = 0
    for char in chars:
        bbox = font.getbbox(char, anchor="lt")
        w = bbox[2] - bbox[0]
        h = bbox[3] - bbox[1]
        if h > max_h: max_h = h
        if w > max_w: max_w = w

    # 大字体防超高逻辑，tiny 不触发
    while not is_tiny and max_h + outline_width * 2 > target_em and real_em > 6:
        real_em -= 1
        font = load_face(font_path, weight, real_em)
        max_h = 0
        max_w = 0
        for char in chars:
            bbox = font.getbbox(char, anchor="lt")
            w = bbox[2] - bbox[0]
            h = bbox[3] - bbox[1]
            if h > max_h: max_h = h
            if w > max_w: max_w = w

    cell_height = int(math.ceil(max_h))
    rows, glyphs = [], {}
    x = 0
    y = 4
    row_height = 0
    SPACING = 6

    for char in chars:
        bbox = font.getbbox(char, anchor="lt")
        left, top, right, bottom = bbox
        width = int(math.ceil(right - left))
        height = int(math.ceil(bottom - top))

        if width <= 0 or height <= 0:
            continue

        if x + width + SPACING * 2 > WIDTH:
            x, y, row_height = 0, y + row_height + SPACING, 0

        glyphs[char] = dict(
            x=x, y=y, w=width, h=height,
            left=round(left * scale),
            top=round(top * scale),
            advance=round(font.getlength(char) * scale),
            scale=scale
        )

        rows.append((char, x, y, width, height))
        x += width + SPACING
        row_height = max(row_height, height)

    atlas_h = y + row_height + SPACING * 2
    atlas = Image.new("RGBA", (WIDTH, atlas_h), (0, 0, 0, 0))

    for char, px, py, w, h in rows:
        char_img = render_colored_glyph(char, font, w, h, outline_width, is_tiny=is_tiny)
        pad = 0 if is_tiny else max(2, outline_width)
        atlas.paste(char_img, (px - pad, py - pad), char_img)

    out_png = OUT / f"{LANGUAGE}-{name}-cjk.png"
    out_json = OUT / f"{LANGUAGE}-{name}-cjk.json"

    atlas.save(out_png, optimize=True)
    with open(out_json, "w", encoding="utf-8", newline="\n") as meta:
        meta.write(json.dumps(
            {"cap": CAP, "glyphs": glyphs},
            separators=(",", ":"), ensure_ascii=False
        ) + "\n")

    print(f"{name}-cjk: real_em={real_em}, font={os.path.basename(font_path)}, max_h={max_h}")


if __name__ == "__main__":
    OUT.mkdir(parents=True, exist_ok=True)
    chars = wanted([CATALOG_JSON])
    for face_name, cfg in ALL_FACE.items():
        build(chars, face_name, cfg["em"], cfg["weight"], cfg["scale"],cfg["fontFile"])