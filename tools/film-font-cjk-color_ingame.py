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
SOURCE_FONT = r"..\..\NotoSansSC.ttf"
CATALOG_JSON = r"..\internal\i18n\locales\chinese.json"
OUT = Path(__file__).resolve().parent.parent / "internal/film/assets"
LANGUAGE="chinese"
WIDTH = 2048

# 这里的 em 代表【描边后】的总高度！
OLD_FACE = {
    # "display": {"em": 121, "weight": 800, "scale": 1.964},
    # "body":    {"em": 125, "weight": 400, "scale": 1.964},
}
NEW_FACE = {
    "small":    {"em": 20, "weight": 800, "scale": 1},
    "normal":   {"em": 28, "weight": 800, "scale": 1},
    "medium":   {"em": 36, "weight": 800, "scale": 1},
    "large":    {"em": 44, "weight": 800, "scale": 1},
    "title":    {"em": 52, "weight": 800, "scale": 1},
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

def get_outline_width(em):
    if em <= 20:    return 1
    elif em <= 30:  return 1
    elif em <= 40:  return 1
    elif em <= 50:  return 1
    elif em <= 60:  return 1
    else:           return 7

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

def render_colored_glyph(char, font, w, h, outline_width):
    pad = max(2, outline_width)
    cell_w = w + pad * 2
    cell_h = h + pad * 2

    # 1. 提取绝对精准的字形遮罩（使用抗锯齿，这决定了字的外形）
    base = Image.new('L', (cell_w, cell_h), 0)
    d = ImageDraw.Draw(base)
    d.text((pad, pad), char, font=font, fill=255, anchor="lt")

    # 2. 画极厚的纯黑描边（类似于 OPTIONS 那种硬边）
    outline = Image.new('L', (cell_w, cell_h), 0)
    do = ImageDraw.Draw(outline)
    r = outline_width
    for dx in range(-r, r + 1):
        for dy in range(-r, r + 1):
            if dx * dx + dy * dy > r * r:
                continue
            do.text((pad + dx, pad + dy), char, font=font, fill=255, anchor="lt")

    # 准备最终输出画布
    res = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))

    # 3. 底层铺纯黑描边
    edge_layer = Image.new('RGBA', (cell_w, cell_h), (*OUTLINE_COLOR, 255))
    res.paste(edge_layer, (0, 0), outline)

    # 4. ★ 核心算法：复刻 OPTIONS 的完美水平柱面光照
    # 先构建一个从上到下的发光渐变（最亮处接近纯白）
    grad = Image.new('L', (cell_w, cell_h), 0)
    center_y = cell_h / 2.0
    max_dist_y = cell_h / 2.0
    if max_dist_y == 0: max_dist_y = 1

    for y in range(cell_h):
        dist = abs(y - center_y)
        norm_dist = dist / max_dist_y
        # 原版你用的 80 衰减。为了达到图里的高对比度，这里把高光提亮到 255，暗部压到底。
        val = 255 - int(norm_dist * 160)
        val = max(0, min(255, val))
        for x in range(cell_w):
            grad.putpixel((x, y), val)

    # 高斯模糊让渐变平滑（图里那种柔和的过渡）
    grad = grad.filter(ImageFilter.GaussianBlur(1.2))

    # 5. 生成字心的颜色（金色渐变字心）
    # 直接用你 PALETTE 里最亮的那一档黄做底色，然后乘以光照渐变
    base_color = PALETTE[0] # (213, 213, 182) 偏白亮的黄

    # 将光照灰度图乘以基础颜色，得到带有光影的 RGB 图像
    color_layer = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))
    px_res = color_layer.load()
    px_base = base.load()
    px_grad = grad.load()

    for y in range(cell_h):
        for x in range(cell_w):
            if px_base[x, y] > 0:
                # 用遮罩的透明度决定边缘平滑，同时用 grad 控制亮度
                alpha = px_base[x, y]
                brightness = px_grad[x, y] / 255.0

                # 叠加光照色
                r = int(base_color[0] * brightness)
                g = int(base_color[1] * brightness)
                b = int(base_color[2] * brightness)
                px_res[x, y] = (r, g, b, alpha)

    # 6. 把带光影的字心，完美覆盖在黑底描边上
    res.alpha_composite(color_layer)

    return res
def build(source, name, chars, target_em, weight, scale):
    CAP = 180
    outline_width = get_outline_width(target_em)

    # 预留描边空间
    real_em = target_em - (outline_width * 2)
    if real_em < 6:
        real_em = 6

    font = load_face(source, weight, real_em)

    # 预扫描：找最大高度和最大宽度
    max_h = 0
    max_w = 0
    for char in chars:
        bbox = font.getbbox(char, anchor="lt")
        w = bbox[2] - bbox[0]
        h = bbox[3] - bbox[1]
        if h > max_h: max_h = h
        if w > max_w: max_w = w

    # ★ 关键：如果加上描边超标，就降低字号，直到包进去为止！
    while max_h + outline_width * 2 > target_em:
        real_em -= 1
        font = load_face(source, weight, real_em)
        max_h = 0
        max_w = 0
        for char in chars:
            bbox = font.getbbox(char, anchor="lt")
            w = bbox[2] - bbox[0]
            h = bbox[3] - bbox[1]
            if h > max_h: max_h = h
            if w > max_w: max_w = w

    # 全局统一使用这个真实的最高、最宽值（强制取整）
    cell_height = int(math.ceil(max_h))

    rows, glyphs = [], {}
    x = 0
    y = 4
    row_height = 0
    SPACING = 6

    for char in chars:
        bbox = font.getbbox(char, anchor="lt")
        left, top, right, bottom = bbox

        # 真实的宽高，强制取整
        width = int(math.ceil(right - left))
        height = int(math.ceil(bottom - top))

        if width <= 0 or height <= 0:
            continue

        if x + width + SPACING * 2 > WIDTH:
            x, y, row_height = 0, y + row_height + SPACING, 0

        # 写入 JSON
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
        char_img = render_colored_glyph(char, font, w, h, outline_width)
        pad = max(2, outline_width)
        # 因为左上角对齐，直接贴图即可，绝不会削顶
        atlas.paste(char_img, (px - pad, py - pad), char_img)

    out_png = OUT / f"{LANGUAGE}-{name}-cjk.png"
    out_json = OUT / f"{LANGUAGE}-{name}-cjk.json"

    atlas.save(out_png, optimize=True)
    with open(out_json, "w", encoding="utf-8", newline="\n") as meta:
        meta.write(json.dumps(
            {"cap": CAP, "glyphs": glyphs},
            separators=(",", ":"), ensure_ascii=False
        ) + "\n")

    print(f"{name}-cjk: target={target_em}, actual_em={real_em}, max_h={max_h}, with_border={max_h + outline_width*2}")

    print(f"{name}-cjk target_em {target_em} (cell {cell_height} + border {outline_width*2} = {cell_height + outline_width*2})")
if __name__ == "__main__":
    OUT.mkdir(parents=True, exist_ok=True)
    chars = wanted([CATALOG_JSON])
    for face_name, cfg in ALL_FACE.items():
        build(SOURCE_FONT, face_name, chars, cfg["em"], cfg["weight"], cfg["scale"])