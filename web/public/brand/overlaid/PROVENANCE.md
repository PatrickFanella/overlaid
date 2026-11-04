# Overlaid brand files

Copied on 2026-11-04 from SUBCULT Studio, `subculture-collective/subcult-tv` at revision `159489c`, path `studio/brands/overlaid/`. That pack is the editing master and its `GUIDE.md` and `alt-text.md` hold the rules and descriptions. Do not edit, redraw or re-export these files here. Copy them again from the pack when it changes.

These files are the app's own identity: page title, icons and link preview. The SUBCULT files in `../subcult/` remain the overlay brand.

## Copied byte for byte

| File here | Pack source | SHA-256 |
| --- | --- | --- |
| `mark.svg` | `logos/mark.svg` | `74de7324fd3ab69550a373ad60d2234624ecc0e7193998362a34d20e355c5a5d` |
| `lockup.svg` | `logos/lockup.svg` | `f2f6316f5797ea24d6e0c20620b2904c67acd0e16f3a1b11e6cf6f8fba77bbfe` |
| `og.png` | `product/og.png` (1200×630) | `13367308ade2f48ad9e6a7dc9bfe21340f0ecfc793c2d145197ad107103bc5fd` |

## Derived from the pack

| File | Made from | SHA-256 |
| --- | --- | --- |
| `favicon.svg` | `mark.svg` with one `#0d0d12` background rectangle inserted after its title; the mark's own elements are unchanged. The ground keeps the light ink visible in light browser tabs | `d62b58a0aebd8bee9065e378b74246a60e36a34f144c2c8a8bd6d6011034fe2d` |
| `../../favicon.ico` | `favicon.svg` rendered with `rsvg-convert` at 16, 32 and 48 px, joined with `magick`; served at `/favicon.ico` | `e3522d40ffd6d8375e4f7cec51ee22b41a7995c234baf946f85347fc3e1bfd93` |
| `apple-touch-icon.png` | `mark.svg` rendered at 180×180 on `#0d0d12` | `1da4bc7c25d6d6472c3ac36856a5f54d046b7ed75887ce7bf401af97be0c1225` |
| `readme-lockup.png` | `lockup.svg` rendered at 770×256 and centred on `#0d0d12` at 898×384, so the light ink stays readable on light README themes | `2ccaefb75c6179c405d88fcfca58274f9699054daaa80fd208e6425d4ccf7278` |

`web/index.html` uses these paths and the pack's link-preview alt text.
