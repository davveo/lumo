#!/usr/bin/env python3
"""Render trusted local SVG covers for the generated demo assortment."""

import html
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "services/content/internal/usecase/category_navigation.json"
TARGET = ROOT / "web/dist/assets"

# Decorative product silhouettes. Labels come from the server seed configuration.
SHAPES = {
    "appliances": '<rect x="62" y="20" width="116" height="190" rx="15"/><path d="M62 92h116M80 44v26m0 42v28"/>',
    "computers": (
        '<rect x="28" y="36" width="184" height="125" rx="10"/>'
        '<path d="M10 182h220l-13 18H23zM120 164v18"/><path d="m94 85 18 18 35-36"/>'
    ),
    "digital": (
        '<rect x="65" y="14" width="110" height="202" rx="21"/><path d="M102 33h36M110 196h20"/>'
        '<circle cx="94" cy="62" r="9"/><circle cx="123" cy="62" r="9"/>'
    ),
    "small-appliances": (
        '<path d="M68 67h103v107c0 24-13 35-51 35s-52-11-52-35zM79 67V47c0-24 82-24 82 0v20M174 '
        '97h18c27 0 27 52-18 52M78 184h85M102 27V9m29 18V9"/>'
    ),
    "food": (
        '<path d="M55 80h130l-9 120H64zM64 80l10-35h92l10 35M86 45V29h68v16"/>'
        '<path d="M97 139c-19-29 27-46 23 1 24-31 49 1 12 15-18 7-32 0-35-16z"/>'
    ),
    "beauty": (
        '<rect x="67" y="77" width="106" height="125" rx="20"/>'
        '<path d="M87 77V49h66v28M96 49V24h48v25M88 107h64v55H88z"/>'
    ),
    "fashion": '<path d="m72 38 28-14c5 26 35 26 40 0l28 14 38 47-33 27-16-17v114H83V95l-16 17-33-27z"/>',
    "sports": (
        '<circle cx="120" cy="116" r="88"/>'
        '<path d="M32 116h176M120 28v176M53 58c88 30 88 86 134 116M53 174c88-30 88-86 134-116"/>'
    ),
    "home": (
        '<path d="M45 118V86c0-47 150-47 150 0v32M47 161h146v36H47zM29 124c0-23 37-23 37 0v37H29zM174 '
        '124c0-23 37-23 37 0v37h-37zM51 198v16m138-16v16M81 104h78"/>'
    ),
    "kitchen": (
        '<path d="M51 77h138v73c0 49-138 49-138 0zM45 77h150M82 60h76M117 60V43M50 94H25v45h26M189 '
        '94h26v45h-26M90 205h60"/>'
    ),
    "renovation": '<path d="M121 23 40 126h162L121 23zM120 126v70M81 208h80M99 23h43"/><path d="M93 157h55"/>',
    "baby": (
        '<path d="m79 63 16-26h50l16 26 34 16-22 41-24-11v56l-29 33-29-33v-56l-24 11-22-41z"/>'
        '<path d="M102 54c0 23 36 23 36 0M90 140h60"/><circle cx="120" cy="107" r="13"/>'
    ),
    "pets": (
        '<ellipse cx="120" cy="153" rx="48" ry="40"/>'
        '<ellipse cx="66" cy="106" rx="19" ry="27" transform="rotate(-26 66 106)"/>'
        '<ellipse cx="98" cy="66" rx="18" ry="26" transform="rotate(-10 98 66)"/>'
        '<ellipse cx="143" cy="68" rx="18" ry="26" transform="rotate(10 143 68)"/>'
        '<ellipse cx="179" cy="107" rx="19" ry="27" transform="rotate(26 179 107)"/>'
    ),
    "books": (
        '<path d="M120 55C88 28 47 28 25 40v155c31-13 67-10 95 10 28-20 64-23 95-10V40c-22-12-63-12-95 '
        '15zM120 55v150M49 74l45 10M49 104l45 10M49 134l45 10M146 84l45-10M146 114l45-10M146 '
        '144l45-10"/>'
    ),
    "jewelry": (
        '<path d="m65 62 28-34h54l28 34-55 68zM65 62h110M93 28l27 34 27-34M120 62v68"/>'
        '<path d="M78 116a61 61 0 1 0 84 0"/>'
    ),
    "shoes": (
        '<path d="M33 141c26-9 46-30 57-63 22 31 45 44 77 50l45 9v45H34zM34 182h178v17H34zM105 109l-15 '
        '18m36-8-14 19m36-10-12 18M41 161h164"/>'
    ),
    "automotive": (
        '<path d="m47 98 20-47h106l20 47 18 17v70H29v-70zM47 98h146M66 185v20H40v-20m134 '
        '0v20h26v-20M91 155h58"/><circle cx="61" cy="132" r="12"/><circle cx="179" cy="132" r="12"/>'
    ),
    "industrial": (
        '<path d="m57 26 39 39-31 31-39-39c-18 43 14 76 51 64l78 78c19 19 49-12 '
        '30-30l-78-78c12-37-21-69-50-65zM161 174l14 14"/>'
    ),
    "garden": (
        '<path d="M74 146h92l-12 62H86zM121 145V54M121 103C69 109 56 72 58 51c36-1 61 18 63 52M121 '
        '81c39 2 63-26 60-53-40 0-60 24-60 53M70 146h100"/>'
    ),
    "wellness": (
        '<rect x="34" y="53" width="172" height="144" rx="20"/>'
        '<path d="M91 53V30h58v23M107 91h26v25h25v26h-25v25h-26v-25H82v-26h25z"/>'
    ),
}


def main():
    groups = json.loads(SOURCE.read_text())["groups"]
    colors = [
        ("#eaf2f2", "#367c7b"),
        ("#edf0f8", "#636f9f"),
        ("#f7efea", "#ae795b"),
        ("#f1edf5", "#88709e"),
    ]
    for index, group in enumerate(groups):
        background, accent = colors[index % len(colors)]
        title = html.escape(group["category"])
        shape = SHAPES.get(group["id"], SHAPES["food"])
        svg = f'''<svg xmlns="http://www.w3.org/2000/svg" width="800" height="800" viewBox="0 0 800 800" role="img" aria-label="{title}演示商品封面">
  <defs><linearGradient id="bg" x2="1" y2="1"><stop stop-color="{background}"/><stop offset="1" stop-color="#fff"/></linearGradient></defs>
  <rect width="800" height="800" fill="url(#bg)"/>
  <circle cx="400" cy="345" r="220" fill="#fff" opacity=".58"/>
  <circle cx="675" cy="155" r="68" fill="{accent}" opacity=".055"/>
  <text x="58" y="70" font-family="Arial,sans-serif" font-size="21" font-weight="700" letter-spacing="5" fill="{accent}">LUMO COLLECTION</text>
  <g transform="translate(228 175) scale(1.44)" fill="none" stroke="{accent}" stroke-width="5" stroke-linecap="round" stroke-linejoin="round">{shape}</g>
  <ellipse cx="400" cy="563" rx="144" ry="14" fill="{accent}" opacity=".07"/>
  <text x="400" y="654" text-anchor="middle" font-family="PingFang SC,Microsoft YaHei,sans-serif" font-size="40" font-weight="600" letter-spacing="8" fill="{accent}">{title}</text>
  <text x="400" y="701" text-anchor="middle" font-family="PingFang SC,Microsoft YaHei,sans-serif" font-size="18" letter-spacing="4" fill="{accent}" opacity=".65">演示商品 · 品类示意图</text>
</svg>
'''
        (TARGET / f'demo-{group["id"]}.svg').write_text(svg)
    print(f"Generated {len(groups)} demo category covers")


if __name__ == "__main__":
    main()
