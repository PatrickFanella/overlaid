# SUBCULT brand files

Copied on 2026-10-05 from `subcult-tv` at commit `bef5a55`, path `studio/brands/subcult/` (production pack v1, direction C "Culture / poster press"). That pack is the editing master. Do not edit, redraw or re-export these files here. Copy them again from the pack when it changes.

| File here | Pack source | Use |
| --- | --- | --- |
| `mark-primary.svg` | `logos/mark-primary.svg` | Ink letters, green block. Purple, green or bone fields. |
| `mark-dark.svg` | `logos/mark-dark.svg` | White letters, outlined block. Ink fields. |
| `mark-mono-dark.svg`, `mark-mono-light.svg` | `logos/` | Single-color marks. |
| `mark-compact-primary.svg`, `mark-compact-dark.svg` | `logos/` | Compact mark D for small placements. |
| `lockup-primary-outlined.svg`, `lockup-dark-outlined.svg` | `logos/` | Horizontal lockup with outlined text. 220px minimum width. |
| `mark-fisheye-circle.png` | `logos/mark-fisheye-circle.png` (Git LFS) | Approved circle logo. SHA-256 `748aaaa135865825f8ada74b4fed74e5c6531c132561523a25b9fcd8eafe8a94`. |
| `*-OFL.txt` | `fonts/` | Font licenses. The app loads the same families from `@fontsource-variable` packages. |

The pack names mark files by the field they sit on, so `mark-dark` has white letters. `src/overlay/Brand.tsx` exposes them as the `ink` and `white` tones.

Rules carried over from the pack's `GUIDE.md`: keep a quarter of the mark's height clear on every side; the mark is at least 32px wide on screen and the lockup at least 220px; green on purple is decorative and never small text.
