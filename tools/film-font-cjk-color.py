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
EXTRA = "简体中文界面ABCDEFGHIJKLMNOPQRSTUVWXYabcdefghijklmnopqrstuvwxyz0123456789"

# 调色板 (64~79) 从亮黄到极暗黄
PALETTE = [
    (251, 251, 215), (229, 223, 189), (195, 195, 155), (171, 171, 131),
    (147, 147, 111), (119, 119, 87), (99, 99, 67), (83, 83, 51),
    (67, 67, 35), (51, 51, 23), (35, 35, 15), (27, 27, 7),
    (23, 23, 7), (19, 19, 0), (15, 15, 0), (11, 11, 0)
]
# 整条调色板统一压暗：0.78 表示亮度乘 0.78，可调 0.7~0.85。
DARKEN = 0.85
PALETTE = [tuple(int(c * DARKEN) for c in color) for color in PALETTE]

# 描边配置
OUTLINE_WIDTH = 5  # 描边像素宽，1/2/3...
OUTLINE_COLOR = (0, 0, 0)  # 描边颜色，纯黑


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
    # 加 ASCII 可打印字符（0x20 空格 到 0x7E ~）
    for code in range(0x20, 0x7F):
        chars.add(chr(code))
    for path in catalogs:
        if not os.path.exists(path): continue
        for text in json.loads(Path(path).read_text(encoding="utf-8")).values():
            chars.update(text)
    # 保留原来的 CJK 过滤，但让 ASCII 也通过，并去掉 { 和 }
    return sorted(c for c in chars if ord(c) >= 0x20 and c not in "{}")

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
    pad = max(2, OUTLINE_WIDTH)
    cell_w = w + pad * 2
    cell_h = h + pad * 2

    draw_x = pad - left
    draw_y = pad - top

    # 1. 基础字形遮罩
    base = Image.new('L', (cell_w, cell_h), 0)
    d = ImageDraw.Draw(base)
    d.text((draw_x, draw_y), char, font=font, fill=255, anchor="ls")

    # 2. 描边：半径 OUTLINE_WIDTH，圆形笔刷
    outline = Image.new('L', (cell_w, cell_h), 0)
    do = ImageDraw.Draw(outline)
    r = OUTLINE_WIDTH
    for dx in range(-r, r + 1):
        for dy in range(-r, r + 1):
            if dx * dx + dy * dy > r * r:  # 圆形笔刷，去掉方角
                continue
            do.text((draw_x + dx, draw_y + dy), char, font=font, fill=255, anchor="ls")

    res = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))

    # 3. 贴描边底色
    edge_layer = Image.new('RGBA', (cell_w, cell_h), (*OUTLINE_COLOR, 255))
    res.paste(edge_layer, (0, 0), outline)

    # 4. 水平柱面光照计算
    ...

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

    # === 1. 预扫描：计算全局的最高点和最低点 ===
    max_top = 0
    max_bottom = 0
    for char in chars:
        left, top, right, bottom = font.getbbox(char, anchor="ls")
        if top < max_top: max_top = top
        if bottom > max_bottom: max_bottom = bottom

    # 统一的图块高度（包含下沉的标点）
    cell_height = max_bottom - max_top

    rows, glyphs = [], {}
    x = row_height = 0
    y = 4

    SPACING = 6

    for char in chars:
        left, top, right, bottom = font.getbbox(char, anchor="ls")
        width = right - left
        if width <= 0:
            continue

        if x + width + SPACING * 2 > WIDTH:
            x, y, row_height = 0, y + row_height + SPACING, 0

        # === 2. 写入 JSON：h 统一为全局高度，top 统一为全局顶部 ===
        glyphs[char] = dict(
            x=x, y=y, w=width, h=cell_height,
            left=round(left * scale), top=round(max_top * scale),
            advance=font.getlength(char) * scale,
            scale=scale
        )

        rows.append((char, x, y, width, cell_height, left, top))

        x += width + SPACING
        row_height = max(row_height, cell_height)

    atlas_h = y + row_height + SPACING * 2
    atlas = Image.new("RGBA", (WIDTH, atlas_h), (0, 0, 0, 0))

    for char, px, py, w, h, left, top in rows:
        # === 3. 渲染时：强制对齐到全局顶部 max_top ===
        # 临时覆盖一下字形顶部的偏移，把实际的 top 替换成全局的 max_top
        # 这样无论逗号多低，它都会在这个统一高度的大框里，画在对应的下方位置
        char_img = render_colored_glyph(char, font, w, h, left, max_top)

        # 注意：因为 render_colored_glyph 内部的 pad 是 max(2, OUTLINE_WIDTH)
        # 如果 OUTLINE_WIDTH 是 5，pad 就是 5。所以 paste 的时候坐标也要用 pad 偏移
        pad = max(2, OUTLINE_WIDTH)
        atlas.paste(char_img, (px - pad, py - pad), char_img)

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