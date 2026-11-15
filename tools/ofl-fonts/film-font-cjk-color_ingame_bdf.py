#!/usr/bin/env python3
import hashlib
import math
import os
import json
import sys
import unicodedata
from pathlib import Path
from PIL import Image, ImageDraw, ImageFilter, ImageChops, ImageFont


# ==========================================
# BDF 字体解析类（新增，只解析BDF点阵）
# ==========================================
class BDFFont:
    def __init__(self, path):
        self.glyphs = dict()
        self.bbx_default = None
        self.ascent = 0
        self.descent = 0
        self.load(path)

    def load(self, path):
        with open(path, "r", encoding="latin-1") as f:
            lines = [line.rstrip("\n") for line in f]
        idx = 0
        while idx < len(lines):
            line = lines[idx]
            idx += 1
            if line.startswith("FONTBOUNDINGBOX"):
                parts = line.split()
                w, h, xoff, yoff = int(parts[1]), int(parts[2]), int(parts[3]), int(parts[4])
                self.bbx_default = (w, h, xoff, yoff)
            elif line.startswith("FONT_ASCENT"):
                self.ascent = int(line.split()[1])
            elif line.startswith("FONT_DESCENT"):
                self.descent = int(line.split()[1])
            elif line.startswith("STARTCHAR"):
                char_name = line.split()[1]
                codepoint = None
                bbx = self.bbx_default
                bitmap_lines = []
                advance = 0
                while idx < len(lines):
                    cl = lines[idx]
                    idx +=1
                    if cl.startswith("ENCODING"):
                        codepoint = int(cl.split()[1])
                    elif cl.startswith("BBX"):
                        p = cl.split()
                        bbx = int(p[1]), int(p[2]), int(p[3]), int(p[4])
                    elif cl.startswith("DWIDTH"):
                        advance = int(cl.split()[1])
                    elif cl.startswith("BITMAP"):
                        while idx < len(lines):
                            bl = lines[idx]
                            idx +=1
                            if bl == "ENDCHAR":
                                break
                            bitmap_lines.append(bl.strip())
                        break
                if codepoint is None or codepoint < 0x20:
                    continue
                ch = chr(codepoint)
                bw, bh, bbx0, bby0 = bbx
                im = Image.new("L", (bw, bh), 0)
                px = im.load()
                for y, hexstr in enumerate(bitmap_lines):
                    val = int(hexstr, 16)
                    for x in range(bw):
                        shift = bw - 1 - x
                        if (val >> shift) & 1:
                            px[x, y] = 255
                self.glyphs[ch] = {
                    "w": bw, "h": bh,
                    "xoff": bbx0, "yoff": bby0,
                    "advance": advance,
                    "bitmap": im
                }

    def getbbox(self, char, anchor="lt"):
        if char not in self.glyphs:
            return None
        g = self.glyphs[char]
        return (0, 0, g["w"], g["h"])

    def getlength(self, char):
        if char not in self.glyphs:
            return 0
        return self.glyphs[char]["advance"]

    def has_char(self, char):
        return char in self.glyphs

# ==========================================
# 1. 强制写死的配置 (输入输出)
# ==========================================
SOURCE_FONT = r"NotoSansSC.ttf"
Pixel_Font_12 = r"Galmuri11.bdf"
Pixel_Font_14 = r"Galmuri14.bdf"  # 改成bdf
PIXEL_FONT_16 = r"Galmuri14.bdf"

CATALOG_JSON = r"..\..\internal\i18n\locales\chinese.json"
OUT = Path(__file__).resolve().parent.parent / r"..\internal/film/assets"
LANGUAGE = "chinese"
WIDTH = 2048

# 定义你要的颜色 (R, G, B)
TARGET_COLOR = (195, 195, 155)
OUTLINE_COLOR = (0, 0, 0)

WEN_QUAN12=r"WenQuanYi.Bitmap.Song.12px.bdf"
WEN_QUAN13=r"WenQuanYi.Bitmap.Song.13px.bdf"
WEN_QUAN14=r"WenQuanYi.Bitmap.Song.14px.bdf"
WEN_QUAN15=r"WenQuanYi.Bitmap.Song.14px.bdf"
WEN_QUAN16=r"WenQuanYi.Bitmap.Song.16px.bdf"

OLD_FACE = {}
# mode 参数："mask" = 纯白蒙版（彻底隔离）, "brush" = 纯色平涂, "render" = 渐变光影
NEW_FACE = {
    "tiny":   {"em": 12, "weight": 800, "scale": 1, "fontFile": Pixel_Font_12, "variable": False, "mode": "mask",   "border": 0, "color": None},
    "small":  {"em": 13, "weight": 800, "scale": 1, "fontFile": Pixel_Font_12, "variable": False, "mode": "render", "border": 1, "color": TARGET_COLOR},
    "normal": {"em": 15, "weight": 800, "scale": 1, "fontFile": Pixel_Font_14, "variable": False, "mode": "render",  "border": 1, "color": TARGET_COLOR},
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
SHADOW_START = 0.2      # 暗部开始的相对距离 (0.0 = 从中心开始暗, 0.5 = 中心一半纯亮，外围才暗)

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

# ==========================================
# 重写 load_face：bdf 走BDFFont，ttf保留旧逻辑
# ==========================================
def load_face(source, weight, size, variable=False):
    src_low = source.lower()
    if src_low.endswith(".bdf"):
        return BDFFont(source)
    # 原来TTF分支保留，兼容
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

# ==========================================
# 渲染mask：适配BDF（直接拿bitmap，不再text绘制）
# ==========================================
def render_mask_glyph(char, font, w, h):
    if isinstance(font, BDFFont):
        g = font.glyphs[char]
        base = g["bitmap"].copy()
    else:
        cell_w = w
        cell_h = h
        base = Image.new('L', (cell_w, cell_h), 0)
        d = ImageDraw.Draw(base)
        d.text((0, 0), char, font=font, fill=255, anchor="lt")
        base = base.point(lambda p: 255 if p > 128 else 0)

    res = Image.new('RGBA', (base.width, base.height), (0, 0, 0, 0))
    px_res = res.load()
    px_base = base.load()
    for y in range(base.height):
        for x in range(base.width):
            if px_base[x, y] == 255:
                px_res[x, y] = (255, 255, 255, 255)
    return res

# ==========================================
# 渲染styled：4方向描边，BDF直接取bitmap，不再text绘制！
# ==========================================
def render_styled_glyph(char, font, w, h, border, mode, color):
    pad = max(2, border)
    cell_w = w + pad * 2
    cell_h = h + pad * 2

    base = Image.new('L', (cell_w, cell_h), 0)
    if isinstance(font, BDFFont):
        g = font.glyphs[char]
        base.paste(g["bitmap"], (pad, pad))
    else:
        d = ImageDraw.Draw(base)
        d.text((pad, pad), char, font=font, fill=255, anchor="lt")
        base = base.point(lambda p: 255 if p > 128 else 0)

    res = Image.new('RGBA', (cell_w, cell_h), (0, 0, 0, 0))

    if border > 0:
        # ========== 4方向硬像素描边 ==========
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
        if isinstance(font, BDFFont):
            if not font.has_char(char):
                continue
            g = font.glyphs[char]
            l, t, r, b = 0, 0, g["w"], g["h"]
        else:
            bbox = font.getbbox(char, anchor="lt")
            if bbox is None:
                continue
            l, t, r, b = bbox
        w = int(math.ceil(r - l))
        if w <= 0:
            continue
        glyph_bbox_info[char] = (l, t, r, b)
        if b > global_bottom:
            global_bottom = b
    if not glyph_bbox_info:
        print(f"[WARN] {name}: 没有可用字形")
        return
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
        if isinstance(font, BDFFont):
            advance = font.glyphs[char]["advance"]
        else:
            advance = font.getlength(char)
        glyphs[char] = dict(
            x=x, y=y, w=width, h=cell_height,
            left=round(left * scale),
            top=round((top + dy) * scale),
            advance=round(advance * scale),
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
