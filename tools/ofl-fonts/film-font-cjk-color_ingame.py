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
OUT = Path(__file__).resolve().parent.parent / r"..\internal/film/assets"
LANGUAGE = "chinese"
WIDTH = 2048

# 定义你要的颜色 (R, G, B)
TARGET_COLOR = (195, 195, 155)
OUTLINE_COLOR = (0, 0, 0)

WEN_QUAN12=r"WenQuanYi.Bitmap.Song.12px.ttf"
WEN_QUAN13=r"WenQuanYi.Bitmap.Song.13px.ttf"
WEN_QUAN14=r"WenQuanYi.Bitmap.Song.14px.ttf"
WEN_QUAN15=r"WenQuanYi.Bitmap.Song.15px.ttf"
WEN_QUAN16=r"WenQuanYi.Bitmap.Song.16px.ttf"

OLD_FACE = {}
# mode 参数："mask" = 纯白蒙版（彻底隔离）, "brush" = 纯色平涂, "render" = 渐变光影
NEW_FACE = {
    "tiny":   {"em": 12, "weight": 800, "scale": 1, "fontFile": Pixel_Font_12, "variable": False, "mode": "mask",   "border": 0, "color": None},
    "small":  {"em": 14, "weight": 800, "scale": 1, "fontFile": PIXEL_FONT_16, "variable": False, "mode": "render", "border": 1, "color": TARGET_COLOR},
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
DARKEN = 1              # 调色板整体压暗系数
SHADOW_STRENGTH = 160   # 边缘最暗处的衰减强度（值越大边缘越黑）
SHADOW_START = 0.4      # 暗部开始的相对距离 (0.0 = 从中心开始暗, 0.5 = 中心一半纯亮，外围才暗)


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


def render_styled_glyph(char, font, w, h, border, mode, color):
    pad = max(2, border)
    cell_w = w + pad * 2
    cell_h = h + pad * 2

    base = Image.new('L', (cell_w, cell_h), 0)
    d = ImageDraw.Draw(base)
    d.text((pad, pad), char, font=font, fill=255, anchor="lt")

    base = base.point(lambda p: 255 if p > 128 else 0)

    res = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))

    if border > 0:
        filter_size = border * 2 + 1
        outline_mask = base.filter(ImageFilter.MaxFilter(filter_size))
        edge_layer = Image.new('RGBA', (cell_w, cell_h), (*OUTLINE_COLOR, 255))
        res.paste(edge_layer, (0, 0), outline_mask)

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
            norm_dist = dist / max_dist_y  # 归一化距离 (0.0 到 1.0)

            # ★ 核心修改：计算带起始范围的渐变比例
            if norm_dist <= SHADOW_START:
                # 离中心太近，不产生暗部，保持纯亮
                shadow_ratio = 0.0
            else:
                # 离中心超过 SHADOW_START 后，重新映射 0.0 ~ 1.0 的比例
                shadow_ratio = (norm_dist - SHADOW_START) / (1.0 - SHADOW_START)

            # 结合衰减强度计算最终亮度
            val = 255 - int(shadow_ratio * SHADOW_STRENGTH)
            val = max(0, min(255, val))

            for x in range(cell_w):
                px_grad[x, y] = val

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


def build(chars, name, target_em, weight, scale, fontFile, variable, mode, border, color):
    CAP = 180

    real_em = target_em
    if real_em < 6:
        real_em = 6

    font = load_face(fontFile, weight, real_em, variable)

    glyph_bbox_info = {}
    global_bottom = -9999
    for char in chars:
        bbox = font.getbbox(char, anchor="lt")
        l, t, r, b = bbox
        w = int(math.ceil(r - l))
        if w <= 0:
            continue
        glyph_bbox_info[char] = (l, t, r, b)
        if b > global_bottom:
            global_bottom = b
    global_top = min(v[1] for v in glyph_bbox_info.values())
    cell_height = global_bottom - global_top

    rows, glyphs = [], {}
    x = 0
    y = 4
    row_height = 0
    SPACING = 6

    for char in chars:
        if char not in glyph_bbox_info:
            continue
        left, top, right, bottom = glyph_bbox_info[char]
        width = int(math.ceil(right - left))

        if x + width + SPACING * 2 > WIDTH:
            x, y, row_height = 0, y + row_height + SPACING, 0

        dy = global_bottom - bottom
        glyphs[char] = dict(
            x=x, y=y, w=width, h=cell_height,
            left=round(left * scale),
            top=round((top + dy) * scale),
            advance=round(font.getlength(char) * scale),
            scale=scale
        )

        rows.append((char, x, y, width, cell_height, left, top, dy))
        x += width + SPACING
        row_height = max(row_height, cell_height)

    atlas_h = y + row_height + SPACING * 2
    atlas = Image.new("RGBA", (WIDTH, atlas_h), (0, 0, 0, 0))

    for char, px, py, w, h, left, top, dy in rows:
        if mode == "mask":
            char_img = render_mask_glyph(char, font, w, h)
            pad = 0
        else:
            char_img = render_styled_glyph(char, font, w, h, border, mode, color)
            pad = max(2, border)

        atlas.paste(char_img, (px - pad, py - pad + dy), char_img)

    out_png = OUT / f"{name}-cjk.png"
    out_json = OUT / f"{name}-cjk.json"

    atlas.save(out_png, optimize=True)
    with open(out_json, "w", encoding="utf-8", newline="\n") as meta:
        meta.write(json.dumps(
            {"cap": CAP, "glyphs": glyphs},
            separators=(",", ":"), ensure_ascii=False
        ) + "\n")

    print(f"{name}-cjk: real_em={real_em}, font={os.path.basename(fontFile)}, mode={mode}, max_h={cell_height},out={out_png}")


if __name__ == "__main__":
    OUT.mkdir(parents=True, exist_ok=True)
    chars = wanted([CATALOG_JSON])
    for face_name, cfg in ALL_FACE.items():
        build(
            chars, face_name, cfg["em"], cfg["weight"], cfg["scale"],
            cfg["fontFile"], cfg.get("variable", False), cfg["mode"], cfg["border"], cfg["color"]
        )