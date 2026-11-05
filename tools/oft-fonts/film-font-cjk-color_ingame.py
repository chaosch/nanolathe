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
SOURCE_FONT = r"NotoSansSC.ttf"
Pixel_Font_12 = r"ZLabsPixel_12px_M_CN.ttf"
PIXEL_FONT_16 = r"ChangBanDianSong-16-SC.ttf"

CATALOG_JSON = r"..\..\internal\i18n\locales\chinese.json"
OUT = Path(__file__).resolve().parent.parent / "..\internal/film/assets"
LANGUAGE = "chinese"
WIDTH = 2048

# 定义你要的颜色 (R, G, B)
TARGET_COLOR = (229, 223, 189)
OUTLINE_COLOR = (0, 0, 0)

OLD_FACE = {}
# mode 参数："render" = 走原版渐变光影，"color" = 纯色平涂，"white" = 纯白点阵
NEW_FACE = {
    "tiny":   {"em": 12, "weight": 800, "scale": 1, "fontFile": Pixel_Font_12, "mode": "white",  "border": 0, "color": None,"is_mask": True},
    "small":  {"em": 14, "weight": 800, "scale": 1, "fontFile": PIXEL_FONT_16, "mode": "render",  "border": 1, "color": TARGET_COLOR,"is_mask": False},
    "normal": {"em": 16, "weight": 800, "scale": 1, "fontFile": PIXEL_FONT_16, "mode": "render", "border": 1, "color": None,"is_mask": False},
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


def render_colored_glyph(char, font, w, h, border, mode, color):
    # tiny 不需要留白，其他字体外扩 border 像素
    pad = 0 if mode == "white" else max(2, border)
    cell_w = w + pad * 2
    cell_h = h + pad * 2

    # 1. 提取字形遮罩
    base = Image.new('L', (cell_w, cell_h), 0)
    d = ImageDraw.Draw(base)
    d.text((pad, pad), char, font=font, fill=255, anchor="lt")

    res = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))

    # ★ 1. tiny：纯白像素（Go 端直接 dynamic 替换为 palColor）
    if mode == "white":
        px_res = res.load()
        px_base = base.load()
        for y in range(cell_h):
            for x in range(cell_w):
                if px_base[x, y] > 128:
                    px_res[x, y] = (255, 255, 255, 255)
        return res

    # --- 以下是纯外围描边逻辑 ---
    px_res = res.load()
    px_base = base.load()

    # 2. 生成纯黑的描边遮罩
    outline_mask = Image.new('L', (cell_w, cell_h), 0)
    px_outline = outline_mask.load()

    if border > 0:
        for y in range(cell_h):
            for x in range(cell_w):
                # 如果当前像素不是字形内部，但周边有字形
                if px_base[x, y] <= 128:
                    is_near_base = False
                    # 检查周围一圈（包含 1px 偏移）
                    for dy in range(-border, border + 1):
                        for dx in range(-border, border + 1):
                            nx, ny = x + dx, y + dy
                            if 0 <= nx < cell_w and 0 <= ny < cell_h:
                                if px_base[nx, ny] > 128:
                                    is_near_base = True
                                    break
                        if is_near_base:
                            break
                    if is_near_base:
                        px_outline[x, y] = 255 # 标记为外围黑边

        # 把外围黑边铺在底层
        edge_layer = Image.new('RGBA', (cell_w, cell_h), (*OUTLINE_COLOR, 255))
        res.paste(edge_layer, (0, 0), outline_mask)

    # 3. 上色逻辑（保证字心原封不动地盖在黑边上方）
    if mode == "color":
        for y in range(cell_h):
            for x in range(cell_w):
                if px_base[x, y] > 128:
                    # 字心纯色
                    px_res[x, y] = (*color, 255)
        return res

    # 4. normal：原有柱面光影逻辑
    if mode == "render":
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
        px_color = color_layer.load()
        px_grad = grad.load()

        for y in range(cell_h):
            for x in range(cell_w):
                # 这里用 px_base > 128 保证字体原本的硬边大小不被吃掉
                if px_base[x, y] > 128:
                    brightness = px_grad[x, y] / 255.0
                    r = int(base_color[0] * brightness)
                    g = int(base_color[1] * brightness)
                    b = int(base_color[2] * brightness)
                    px_color[x, y] = (r, g, b, 255)

        # 将带光影的字心盖在刚才的黑边上面
        res.alpha_composite(color_layer)
        return res

    return res


def build(chars, name, target_em, weight, scale, fontFile, mode, border, color):
    CAP = 180

    # 只要是独立配置的点阵字体，强制用原生尺寸
    is_native_size = True
    real_em = target_em

    if real_em < 6:
        real_em = 6

    font = load_face(fontFile, weight, real_em)

    max_h = 0
    max_w = 0
    for char in chars:
        bbox = font.getbbox(char, anchor="lt")
        w = bbox[2] - bbox[0]
        h = bbox[3] - bbox[1]
        if h > max_h: max_h = h
        if w > max_w: max_w = w

    # 大字体防超高逻辑（点阵字体不走这个）
    if not is_native_size:
        while max_h + border * 2 > target_em and real_em > 6:
            real_em -= 1
            font = load_face(fontFile, weight, real_em)
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
        char_img = render_colored_glyph(char, font, w, h, border, mode, color)
        pad = 0 if mode == "white" else max(2, border)
        atlas.paste(char_img, (px - pad, py - pad), char_img)

    out_png = OUT / f"{name}-cjk.png"
    out_json = OUT / f"{name}-cjk.json"

    atlas.save(out_png, optimize=True)
    with open(out_json, "w", encoding="utf-8", newline="\n") as meta:
        meta.write(json.dumps(
            {"cap": CAP, "glyphs": glyphs},
            separators=(",", ":"), ensure_ascii=False
        ) + "\n")

    print(f"{name}-cjk: real_em={real_em}, font={os.path.basename(fontFile)}, mode={mode}, max_h={max_h},out={out_png}")


if __name__ == "__main__":
    OUT.mkdir(parents=True, exist_ok=True)
    chars = wanted([CATALOG_JSON])
    for face_name, cfg in ALL_FACE.items():
        build(chars, face_name, cfg["em"], cfg["weight"], cfg["scale"], cfg["fontFile"], cfg["mode"], cfg["border"], cfg["color"])