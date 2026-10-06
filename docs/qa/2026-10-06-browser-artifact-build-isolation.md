# Browser artifact build isolation, October 6, 2026

Source baseline: `8355926`. Browser results were untracked in Git and included in Tailwind's automatic source scan. During the profile acceptance work, Vite reported 53.78 kB of production CSS with generated error context present and 51.14 kB after cleanup. A controlled fixture confirmed the cause.

## Reproduction and correction

1. Built with no browser artifacts. Production CSS was 51,135 bytes, SHA-256 `c8bcba2d5801467ed70503b1dedf696a5b0a6ecec8433db4b9160c9ecdaa312e`.
2. Wrote a synthetic HTML fixture containing a unique arbitrary padding utility under `web/test-results`. The next build produced 51,162 bytes and included its 139px declaration. No product source changed.
3. Added `web/test-results/` and `web/playwright-report/` to `.gitignore`. The existing web `.dockerignore` already excludes both directories.
4. Added another synthetic report fixture with a distinct 137px utility. With both artifacts present, the build returned to the exact artifact-free CSS bytes and hash; neither synthetic declaration remained. `git check-ignore -v` confirmed both paths.
5. Removed the owned fixture files. Generated browser traces/reports remain local and ignored.

[Tailwind's source-detection documentation](https://tailwindcss.com/docs/detecting-classes-in-source-files) identifies Git-ignored files as excluded from the default scan. The controlled builds above verify that behavior in this worktree.

`make verify` passed with 113 frontend tests. All 23 `make smoke-browser` workflows passed after the exclusion change. The ignore rules prevent generated browser evidence from entering commits and keep production CSS independent of that evidence. No product styles or browser behavior were changed.
