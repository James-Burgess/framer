#!/usr/bin/env python3
"""
Builds both:
  1) dist/resource.custom.img — replaces the red "Rayswitch x Frameo" U-Boot/kernel
     splash (logo.bmp & logo_kernel.bmp) in-place with a matching obsidian-black +
     subtle warm-gold glow 800x1280 24-bit BMP (exact 3,072,054 bytes, zero DTB edits).
  2) dist/bootanimation.zip — cinematic 1280x800 24fps Android boot animation:
     - part0 (116 frames, plays once): smoothly writes out
       "Thanks for being the best dad ever!" in clean Optima typography with a
       warm gold ink glow & expanding accent line, holds warmly, dissolves, and
       reveals the animated Burgess (England) family crest with a metallic sheen.
     - part1 (48 frames, loops): gentle breathing golden halo + floating gold motes
       + soft periodic glint on the Burgess crest until frmaeo launches.
"""
from io import BytesIO
import math
import os
import zipfile
from PIL import Image, ImageChops, ImageDraw, ImageEnhance, ImageFilter, ImageFont

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC_IMG = os.path.join(ROOT, "assets", "burgess_crest.jpg")
BACKUP_RES = os.path.join(ROOT, "backups", "resource.img")
OUT_RES = os.path.join(ROOT, "dist", "resource.custom.img")
OUT_ZIP = os.path.join(ROOT, "dist", "bootanimation.zip")

W, H = 1280, 800
FPS = 24
MESSAGE = "Thanks for being the best dad ever!"

FONT_CANDIDATES = [
    "/System/Library/Fonts/Optima.ttc",
    "/System/Library/Fonts/Avenir Next.ttc",
    "/System/Library/Fonts/Supplemental/Georgia.ttf",
]


def load_font(size: int):
    for p in FONT_CANDIDATES:
        if os.path.exists(p):
            return ImageFont.truetype(p, size)
    return ImageFont.load_default()


def prepare_base(path: str) -> Image.Image:
    im = Image.open(path).convert("RGB")
    sw, sh = im.size
    target_aspect = W / H
    cur_aspect = sw / sh
    if cur_aspect > target_aspect:
        new_w = int(round(sh * target_aspect))
        left = (sw - new_w) // 2
        im = im.crop((left, 0, left + new_w, sh))
    else:
        new_h = int(round(sw / target_aspect))
        top = (sh - new_h) // 2
        im = im.crop((0, top, sw, top + new_h))
    return im.resize((W, H), Image.Resampling.LANCZOS)


def make_crest_mask(base: Image.Image) -> Image.Image:
    gray = base.convert("L")
    lut = [0 if i < 22 else min(255, int((i - 22) * 1.35)) for i in range(256)]
    return gray.point(lut).filter(ImageFilter.GaussianBlur(radius=2))


def make_glow_layer() -> Image.Image:
    glow = Image.new("RGB", (W, H), (0, 0, 0))
    draw = ImageDraw.Draw(glow)
    cx, cy = W // 2, H // 2
    for r in range(360, 10, -12):
        k = (1.0 - r / 360.0) ** 2.2
        col = (int(56 * k), int(38 * k), int(12 * k))
        draw.ellipse([cx - r, cy - r, cx + r, cy + r], fill=col)
    return glow.filter(ImageFilter.GaussianBlur(radius=32))


def make_sheen_layer(progress: float, strength: float, crest_mask: Image.Image) -> Image.Image:
    sheen = Image.new("RGB", (W, H), (0, 0, 0))
    draw = ImageDraw.Draw(sheen)
    x_center = int(-320 + progress * (W + 640))
    band_w = 110
    col = (int(130 * strength), int(112 * strength), int(72 * strength))
    pts = [
        (x_center - band_w, H),
        (x_center + band_w, H),
        (x_center + band_w + 340, 0),
        (x_center - band_w + 340, 0),
    ]
    draw.polygon(pts, fill=col)
    sheen = sheen.filter(ImageFilter.GaussianBlur(radius=38))
    black = Image.new("RGB", (W, H), (0, 0, 0))
    return Image.composite(sheen, black, crest_mask)


MOTES = [
    (360, 650, 2.2, 0.00),
    (430, 520, 1.8, 0.17),
    (860, 680, 2.4, 0.31),
    (920, 500, 1.7, 0.48),
    (490, 710, 2.0, 0.62),
    (790, 610, 1.9, 0.77),
    (390, 410, 1.5, 0.85),
    (890, 380, 1.6, 0.23),
    (560, 740, 1.8, 0.41),
    (730, 730, 2.1, 0.55),
]


def draw_motes(frame: Image.Image, phase: float, opacity: float) -> Image.Image:
    if opacity <= 0.01:
        return frame
    mote_layer = Image.new("RGB", (W, H), (0, 0, 0))
    draw = ImageDraw.Draw(mote_layer)
    for mx, my, rad, offset in MOTES:
        t = (phase + offset) % 1.0
        y = my - t * 140
        x = mx + math.sin(t * math.tau + offset * 6) * 14
        alpha = math.sin(t * math.pi) * opacity
        c = (int(225 * alpha), int(182 * alpha), int(95 * alpha))
        draw.ellipse([x - rad, y - rad, x + rad, y + rad], fill=c)
    mote_layer = mote_layer.filter(ImageFilter.GaussianBlur(radius=1.2))
    return ImageChops.add(frame, mote_layer)


def scale_centered(img: Image.Image, scale: float) -> Image.Image:
    if abs(scale - 1.0) < 0.001:
        return img
    nw = int(round(W * scale))
    nh = int(round(H * scale))
    resized = img.resize((nw, nh), Image.Resampling.LANCZOS)
    canvas = Image.new("RGB", (W, H), (0, 0, 0))
    canvas.paste(resized, ((W - nw) // 2, (H - nh) // 2))
    return canvas


def prepare_message_layer():
    font = load_font(58)
    layer = Image.new("RGB", (W, H), (0, 0, 0))
    draw = ImageDraw.Draw(layer)
    bbox = draw.textbbox((0, 0), MESSAGE, font=font)
    tw = bbox[2] - bbox[0]
    th = bbox[3] - bbox[1]
    tx = (W - tw) // 2 - bbox[0]
    ty = (H - th) // 2 - bbox[1] - 10
    draw.text((tx, ty), MESSAGE, font=font, fill=(246, 242, 235))
    return layer, tx, tx + tw, ty + th + 28


def render_message_frame(
    msg_layer: Image.Image,
    x_start: int,
    x_end: int,
    line_y: int,
    glow: Image.Image,
    reveal: float,
    alpha: float,
    phase: float,
) -> Image.Image:
    # Base obsidian + subtle warm aura (matches resource.img splash seamlessly)
    bg = ImageEnhance.Brightness(glow).enhance(0.45 + 0.25 * alpha)

    if reveal <= 0.0 or alpha <= 0.01:
        return bg

    # Soft-edged horizontal write-on mask
    mask = Image.new("L", (W, H), 0)
    mdraw = ImageDraw.Draw(mask)
    cur_x = int(x_start + (x_end - x_start + 16) * min(1.0, reveal))
    mdraw.rectangle([0, 0, cur_x, H], fill=255)
    mask = mask.filter(ImageFilter.GaussianBlur(radius=14))

    revealed_text = Image.composite(msg_layer, Image.new("RGB", (W, H), (0, 0, 0)), mask)
    if alpha < 0.99:
        revealed_text = ImageEnhance.Brightness(revealed_text).enhance(alpha)

    frame = ImageChops.add(bg, revealed_text)

    # Subtle golden spark glow at the active writing edge while writing
    if 0.02 < reveal < 0.98:
        tip = Image.new("RGB", (W, H), (0, 0, 0))
        tdraw = ImageDraw.Draw(tip)
        cy = H // 2 - 6
        tdraw.ellipse([cur_x - 24, cy - 36, cur_x + 24, cy + 36], fill=(120, 88, 36))
        tip = tip.filter(ImageFilter.GaussianBlur(radius=22))
        frame = ImageChops.add(frame, tip)

    # Delicate expanding golden accent line beneath the sentence
    line_progress = max(0.0, min(1.0, (reveal - 0.12) / 0.88))
    if line_progress > 0.01:
        line_layer = Image.new("RGB", (W, H), (0, 0, 0))
        ldraw = ImageDraw.Draw(line_layer)
        half = int(210 * line_progress)
        cx = W // 2
        c = (int(224 * alpha), int(164 * alpha), int(86 * alpha))
        ldraw.line([(cx - half, line_y), (cx + half, line_y)], fill=c, width=2)
        frame = ImageChops.add(frame, line_layer)

    return draw_motes(frame, phase, opacity=0.35 * alpha)


def draw_heart(draw: ImageDraw.ImageDraw, cx: float, cy: float, size: float, fill):
    # Smooth parametric heart polygon
    pts = []
    for k in range(60):
        t = k * math.tau / 60.0
        x = 16 * (math.sin(t) ** 3)
        y = -(13 * math.cos(t) - 5 * math.cos(2 * t) - 2 * math.cos(3 * t) - math.cos(4 * t))
        pts.append((cx + x * size / 16.0, cy + y * size / 16.0))
    draw.polygon(pts, fill=fill)


def build_custom_resource_img(glow: Image.Image):
    if not os.path.exists(BACKUP_RES):
        print(f"Skipping resource.img patch ({BACKUP_RES} not found)")
        return

    # Deep obsidian canvas with gentle warm golden glow + "Built by James Burgess with ♥"
    splash_1280 = ImageEnhance.Brightness(glow).enhance(0.50).copy()
    draw = ImageDraw.Draw(splash_1280)
    font = load_font(42)
    label = "Built by James Burgess with"
    bbox = draw.textbbox((0, 0), label, font=font)
    tw = bbox[2] - bbox[0]
    th = bbox[3] - bbox[1]
    heart_gap = 18
    heart_w = 34
    total_w = tw + heart_gap + heart_w
    tx = (W - total_w) // 2 - bbox[0]
    ty = (H - th) // 2 - bbox[1]
    draw.text((tx, ty), label, font=font, fill=(240, 234, 224))
    draw_heart(draw, tx + bbox[0] + tw + heart_gap + heart_w / 2.0, H / 2.0 + 2, 17.5, fill=(240, 106, 123))

    # Subtle golden hairline rule below
    rule_y = (H + th) // 2 + 28
    draw.line([(W // 2 - 90, rule_y), (W // 2 + 90, rule_y)], fill=(185, 138, 72), width=2)

    splash_1280.save("/tmp/splash_preview.png")

    # Rotate 90 deg CW to match stock 800x1280 panel orientation
    splash_800 = splash_1280.rotate(-90, expand=True).convert("RGB")
    buf = BytesIO()
    splash_800.save(buf, format="BMP")
    bmp_bytes = buf.getvalue()
    assert len(bmp_bytes) == 3072054, f"Expected 3072054-byte BMP, got {len(bmp_bytes)}"

    res = bytearray(open(BACKUP_RES, "rb").read())
    # Patch logo.bmp (block 216) and logo_kernel.bmp (block 6217) in-place
    for blk in (216, 6217):
        off = blk * 512
        assert res[off : off + 2] == b"BM", f"Expected BMP magic at block {blk}"
        res[off : off + len(bmp_bytes)] = bmp_bytes

    with open(OUT_RES, "wb") as f:
        f.write(res)
    print(f"Built {OUT_RES} ({len(res) / (1024*1024):.2f} MB, patched logo.bmp & logo_kernel.bmp)")


def save_frame(zf: zipfile.ZipFile, arcname: str, img: Image.Image):
    buf = BytesIO()
    img.save(buf, format="JPEG", quality=84, optimize=True)
    zf.writestr(arcname, buf.getvalue())


def main():
    os.makedirs(os.path.dirname(OUT_ZIP), exist_ok=True)
    base = prepare_base(SRC_IMG)
    crest_mask = make_crest_mask(base)
    glow = make_glow_layer()
    msg_layer, x_start, x_end, line_y = prepare_message_layer()

    # 1. Build patched resource.img (removes Frameo x Rayswitch U-Boot/kernel splash)
    build_custom_resource_img(glow)

    # Save a quick preview frame of the completed message
    preview = render_message_frame(msg_layer, x_start, x_end, line_y, glow, 1.0, 1.0, 0.35)
    preview.save("/tmp/dad_msg_preview.png")

    # 2. Build animated bootanimation.zip
    with zipfile.ZipFile(OUT_ZIP, "w", compression=zipfile.ZIP_STORED) as zf:
        desc = f"{W} {H} {FPS}\np 1 0 part0\np 0 0 part1\n"
        zf.writestr("desc.txt", desc)

        idx = 0
        # 0..55 (2.3s): Smooth write-on of "Thanks for being the best dad ever!"
        for i in range(56):
            t = i / 55.0
            # Gentle ease-in-out writing pace
            reveal = t * t * (3 - 2 * t)
            frame = render_message_frame(msg_layer, x_start, x_end, line_y, glow, reveal, 1.0, i / 120.0)
            save_frame(zf, f"part0/{idx:03d}.jpg", frame)
            idx += 1

        # 56..77 (0.9s): Warm hold
        for i in range(22):
            frame = render_message_frame(msg_layer, x_start, x_end, line_y, glow, 1.0, 1.0, (56 + i) / 120.0)
            save_frame(zf, f"part0/{idx:03d}.jpg", frame)
            idx += 1

        # 78..89 (0.5s): Graceful dissolve
        for i in range(12):
            alpha = 1.0 - (i + 1) / 12.0
            frame = render_message_frame(msg_layer, x_start, x_end, line_y, glow, 1.0, alpha, (78 + i) / 120.0)
            save_frame(zf, f"part0/{idx:03d}.jpg", frame)
            idx += 1

        # 90..115 (1.1s): Heroic Burgess crest fade-in + scale-up + sheen sweep
        n_crest = 26
        for i in range(n_crest):
            t = i / (n_crest - 1)
            ease = 1.0 - (1.0 - t) ** 3
            scale = 0.93 + 0.07 * ease
            frame = scale_centered(base, scale)
            frame = ImageEnhance.Brightness(frame).enhance(ease)
            b_glow = ImageEnhance.Brightness(glow).enhance(0.45 + 0.25 * ease)
            frame = ImageChops.add(frame, b_glow)
            if t >= 0.42:
                st = (t - 0.42) / 0.58
                if st <= 1.0:
                    sheen = make_sheen_layer(st, 0.95 * math.sin(st * math.pi), crest_mask)
                    frame = ImageChops.add(frame, sheen)
            frame = draw_motes(frame, t * 0.4, opacity=0.45 * ease)
            save_frame(zf, f"part0/{idx:03d}.jpg", frame)
            idx += 1

        # part1: 48 frames (2.0s) — seamless regal breathing glow + soft periodic glint
        n1 = 48
        for i in range(n1):
            phase = i / n1
            breath = 0.5 + 0.5 * math.sin(phase * math.tau)
            frame = ImageEnhance.Brightness(base).enhance(0.98 + 0.05 * breath)
            b_glow = ImageEnhance.Brightness(glow).enhance(0.55 + 0.45 * breath)
            frame = ImageChops.add(frame, b_glow)
            if phase < 0.45:
                st = phase / 0.45
                sheen = make_sheen_layer(st, 0.55 * math.sin(st * math.pi), crest_mask)
                frame = ImageChops.add(frame, sheen)
            frame = draw_motes(frame, phase, opacity=0.55)
            save_frame(zf, f"part1/{i:03d}.jpg", frame)

    sz = os.path.getsize(OUT_ZIP)
    print(f"Built {OUT_ZIP} ({sz / (1024*1024):.2f} MB, {idx}+{n1} frames @ {FPS}fps)")


if __name__ == "__main__":
    main()
