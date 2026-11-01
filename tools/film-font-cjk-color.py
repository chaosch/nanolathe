#!/usr/bin/env python3
import hashlib
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

WIDTH = 2048
FORCED_EM = {"display": 121, "body": 125}
FORCED_SCALE = 1.964
WEIGHT = {"display": 800, "body": 400}
EXTRA = "简体中文界面"

# 调色板 (64~79) 从亮黄到极暗黄
PALETTE = [
    (251, 251, 215), (229, 223, 189), (195, 195, 155), (171, 171, 131),
    (147, 147, 111), (119, 119, 87), (99, 99, 67), (83, 83, 51),
    (67, 67, 35), (51, 51, 23), (35, 35, 15), (27, 27, 7),
    (23, 23, 7), (19, 19, 0), (15, 15, 0), (11, 11, 0)
]
EDGE_COLOR = PALETTE[-1]  # 描边用最黑


def gray_to_palette(gray_val):
    if gray_val == 0: return (0, 0, 0)

    # ★ 关键修改 1：调整指数为 0.8。
    # 这会让亮度映射提前进入高光区，减少暗部颜色(70+)的出现频率。
    ratio = (gray_val / 255.0) ** 0.8

    idx = int((1.0 - ratio) * (len(PALETTE) - 1))
    idx = max(0, min(idx, len(PALETTE) - 1))
    return PALETTE[idx]


def wanted(catalogs):
    chars = set(EXTRA)
    for path in catalogs:
        if not os.path.exists(path): continue
        for text in json.loads(Path(path).read_text(encoding="utf-8")).values():
            chars.update(text)
    return sorted(c for c in chars if ord(c) > 0x7F and unicodedata.east_asian_width(c) in "WF")


def load_face(source, weight, size):
    font = ImageFont.truetype(source, size)
    try:
        font.set_variation_by_axes([weight])
    except:
        pass
    return font


# ==========================================
# 2. 核心渲染 (水平柱面光照 + 细描边)
# ==========================================
def render_colored_glyph(char, font, w, h, left, top):
    pad = 2
    cell_w = w + pad * 2
    cell_h = h + pad * 2

    draw_x = pad - left
    draw_y = pad - top

    # 1. 基础字形遮罩
    base = Image.new('L', (cell_w, cell_h), 0)
    d = ImageDraw.Draw(base)
    d.text((draw_x, draw_y), char, font=font, fill=255, anchor="ls")

    # 2. 1像素极细描边
    outline = Image.new('L', (cell_w, cell_h), 0)
    do = ImageDraw.Draw(outline)
    for dx in [-1, 0, 1]:
        for dy in [-1, 0, 1]:
            do.text((draw_x + dx, draw_y + dy), char, font=font, fill=255, anchor="ls")

    res = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))

    # 3. 贴描边底色
    edge_layer = Image.new('RGBA', (cell_w, cell_h), (*EDGE_COLOR, 255))
    res.paste(edge_layer, (0, 0), outline)

    # 4. 水平柱面光照计算
    center_y = cell_h / 2.0
    max_dist_y = cell_h / 2.0
    if max_dist_y == 0: max_dist_y = 1

    grad = Image.new('L', (cell_w, cell_h), 0)

    for y in range(cell_h):
        dist = abs(y - center_y)
        norm_dist = dist / max_dist_y

        # ★ 关键修改 2：衰减系数从 135 降到 80。
        # 使得字体上下边缘的亮度更高，暗部极度压缩。
        val = 255 - int(norm_dist * 80)
        val = max(0, min(255, val))

        for x in range(cell_w):
            grad.putpixel((x, y), val)

    grad = grad.filter(ImageFilter.GaussianBlur(1.5))

    # 5. 切割光影
    shaded_gray = ImageChops.multiply(base, grad)

    # 6. 上色
    px_gray = shaded_gray.load()
    px_res = res.load()
    base_px = base.load()

    for y in range(cell_h):
        for x in range(cell_w):
            if base_px[x, y] > 0:
                g = px_gray[x, y]
                color = gray_to_palette(g)
                px_res[x, y] = (*color, 255)

    return res


# ==========================================
# 3. 图集构建
# ==========================================
def build(source, name, chars):
    base_json_path = OUT / (name + ".json")
    if not base_json_path.exists():
        print(f"错误: 找不到基准 JSON {base_json_path}")
        return

    cap = json.loads(base_json_path.read_text(encoding="utf-8"))["cap"]
    em = FORCED_EM[name]
    scale = FORCED_SCALE
    font = load_face(source, WEIGHT[name], em)

    rows, glyphs = [], {}
    x = row_height = 0
    y = 4

    SPACING = 6

    for char in chars:
        left, top, right, bottom = font.getbbox(char, anchor="ls")
        width, height = right - left, bottom - top
        if width <= 0 or height <= 0: continue

        if x + width + SPACING * 2 > WIDTH:
            x, y, row_height = 0, y + row_height + SPACING, 0

        glyphs[char] = dict(
            x=x, y=y, w=width, h=height,
            left=round(left * scale), top=round(top * scale),
            advance=font.getlength(char) * scale,
            scale=scale
        )

        rows.append((char, x, y, width, height, left, top))

        x += width + SPACING
        row_height = max(row_height, height)

    atlas_h = y + row_height + SPACING * 2
    atlas = Image.new("RGBA", (WIDTH, atlas_h), (0, 0, 0, 0))

    for char, px, py, w, h, left, top in rows:
        char_img = render_colored_glyph(char, font, w, h, left, top)
        atlas.paste(char_img, (px - 2, py - 2), char_img)

    out_png = OUT / f"{name}-cjk.png"
    out_json = OUT / f"{name}-cjk.json"

    atlas.save(out_png, optimize=True)
    with open(out_json, "w", encoding="utf-8", newline="\n") as meta:
        meta.write(json.dumps(
            {"cap": cap, "glyphs": glyphs},
            separators=(",", ":"), ensure_ascii=False
        ) + "\n")

    print(f"{name}-cjk em {em} scale {round(scale, 3)} glyphs {len(glyphs)} atlas {atlas.size}")


if __name__ == "__main__":
    OUT.mkdir(parents=True, exist_ok=True)
    chars = wanted([CATALOG_JSON])
    build(SOURCE_FONT, "display", chars)
    build(SOURCE_FONT, "body", chars)