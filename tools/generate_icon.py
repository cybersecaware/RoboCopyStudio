#!/usr/bin/env python3
"""Generate the original RoboCopy Studio identity; requires Pillow only.

Re-run explicitly when editing the artwork. Normal Go builds use the checked-in
ICO and do not require Python imaging packages or external assets.
"""
from pathlib import Path
from PIL import Image, ImageDraw

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / "assets"
SIZES = (16, 20, 24, 32, 40, 48, 64, 96, 128, 256)
SVG = '''<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024" viewBox="0 0 256 256" role="img" aria-labelledby="title desc">
<title id="title">RoboCopy Studio</title>
<desc id="desc">Two overlapping file panels and a forward transfer arrow, cyan and teal on a rounded midnight-blue tile.</desc>
<defs><linearGradient id="tile" x1="0" y1="0" x2="0" y2="1"><stop stop-color="#1c3044"/><stop offset="1" stop-color="#0e1521"/></linearGradient></defs>
<rect x="12" y="12" width="232" height="232" rx="54" fill="url(#tile)" stroke="#30485e" stroke-width="2"/>
<rect x="57" y="55" width="94" height="122" rx="14" fill="#122337" stroke="#41647e" stroke-width="5"/>
<rect x="83" y="79" width="94" height="122" rx="14" fill="#112d3c" stroke="#75ddea" stroke-width="6"/>
<path d="M105 105H151M105 124H134" fill="none" stroke="#799dad" stroke-width="6" stroke-linecap="round"/>
<path d="M105 157H201M180 136L201 157L180 178" fill="none" stroke="#48d7b7" stroke-width="12" stroke-linecap="round" stroke-linejoin="round"/>
</svg>\n'''


def make_icon():
    scale = 4
    n = 256 * scale
    image = Image.new("RGBA", (n, n))
    gradient = Image.new("RGBA", (n, n))
    draw = ImageDraw.Draw(gradient)
    top, bottom = (28, 48, 68), (14, 21, 33)
    for y in range(n):
        t = min(1.0, max(0.0, (y / scale - 12) / 232))
        color = tuple(round(a * (1 - t) + b * t) for a, b in zip(top, bottom))
        draw.line((0, y, n, y), fill=color + (255,))
    mask = Image.new("L", (n, n))
    md = ImageDraw.Draw(mask)
    box = lambda coords: tuple(round(x * scale) for x in coords)
    md.rounded_rectangle(box((12, 12, 244, 244)), radius=54 * scale, fill=255)
    image.paste(gradient, (0, 0), mask)
    d = ImageDraw.Draw(image)
    d.rounded_rectangle(box((12, 12, 244, 244)), radius=54 * scale, outline="#30485e", width=2 * scale)
    d.rounded_rectangle(box((57, 55, 151, 177)), radius=14 * scale, fill="#122337", outline="#41647e", width=5 * scale)
    d.rounded_rectangle(box((83, 79, 177, 201)), radius=14 * scale, fill="#112d3c", outline="#75ddea", width=6 * scale)

    def stroke(points, color, width):
        pts = [box(p) for p in points]
        d.line(pts, fill=color, width=width * scale, joint="curve")
        radius = width * scale / 2
        for x, y in pts:
            d.ellipse((round(x-radius), round(y-radius), round(x+radius), round(y+radius)), fill=color)

    stroke(((105, 105), (151, 105)), "#799dad", 6)
    stroke(((105, 124), (134, 124)), "#799dad", 6)
    stroke(((105, 157), (201, 157)), "#48d7b7", 12)
    stroke(((180, 136), (201, 157), (180, 178)), "#48d7b7", 12)
    return image


def main():
    OUT.mkdir(exist_ok=True)
    (OUT / "robocopy-studio.svg").write_text(SVG, encoding="utf-8")
    image = make_icon()
    image.save(OUT / "robocopy-studio.png", optimize=True)
    image.save(OUT / "robocopy-studio.ico", format="ICO", sizes=[(x, x) for x in SIZES])
    # Pillow writes zero planes for PNG payloads; make the ICO directory's
    # informational color-plane field explicitly canonical (one plane).
    import struct
    ico_path = OUT / "robocopy-studio.ico"
    ico_data = bytearray(ico_path.read_bytes())
    for i in range(len(SIZES)):
        struct.pack_into("<H", ico_data, 6 + i * 16 + 4, 1)
    ico_path.write_bytes(ico_data)
    # A readable inspection sheet at real Windows sizes on light and dark surfaces.
    preview = Image.new("RGB", (1000, 410), "#0e1118")
    d = ImageDraw.Draw(preview)
    d.rectangle((500, 0, 1000, 410), fill="#eef3f8")
    for offset, color in ((0, "#d9e9f5"), (500, "#25374a")):
        preview.paste(image.resize((256, 256), Image.Resampling.LANCZOS), (offset+122, 20), image.resize((256, 256), Image.Resampling.LANCZOS))
        x = offset + 48
        for size in (16, 24, 32, 48, 64, 96):
            thumb = image.resize((size, size), Image.Resampling.LANCZOS)
            preview.paste(thumb, (x, 304+(96-size)//2), thumb)
            x += size + 12
    preview.save(OUT / "icon-preview.png", optimize=True)
    with Image.open(OUT / "robocopy-studio.ico") as icon:
        assert icon.ico.sizes() == {(s, s) for s in SIZES}
        for size in SIZES:
            frame = icon.ico.getimage((size, size))
            assert frame.size == (size, size) and frame.getextrema()[3][0] == 0
    print("Generated SVG, 1024px PNG, preview and ICO frames:", SIZES)


if __name__ == "__main__":
    main()
