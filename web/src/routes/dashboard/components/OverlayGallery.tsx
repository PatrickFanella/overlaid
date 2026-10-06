import { buildOverlaySourceUrl, overlayRoutes } from '../../../lib/overlayRoutes';

type OverlaySourceCardsProps = {
  apiBaseUrl: string;
  onCopy: (label: string, value: string) => void;
};

export function OverlaySourceCards({ apiBaseUrl, onCopy }: OverlaySourceCardsProps) {
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Overlay source URLs</p>
      <h2 className="hud-section-title">Copy into OBS</h2>
      <p className="hud-copy mt-1 text-xs text-zinc-300">Browser-source URLs for each route.</p>

      <div className="mt-3 grid gap-2 xl:grid-cols-2">
        {overlayRoutes.map((route) => {
          const url = buildOverlaySourceUrl(route.path, apiBaseUrl);

          return (
            <article key={route.path} className="hud-panel hud-corners hud-panel--flat hud-compact-card">
              <div className="flex flex-col gap-2">
                <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
                  <div>
                    <p className="hud-kicker">{route.defaultScene} scene</p>
                    <h3 className="mt-1 text-sm font-bold text-white">{route.title}</h3>
                    <p className="mt-1 text-xs text-zinc-300">{route.description}</p>
                  </div>
                  <button className="hud-chip hud-chip--square whitespace-nowrap" type="button" onClick={() => onCopy(route.title, url)}>
                    Copy URL
                  </button>
                </div>

                <code className="hud-mono break-all border border-white/10 bg-black/30 px-2 py-1 text-xs text-cyan-100">{url}</code>
                <p className="hud-kicker">Path {route.path}</p>
              </div>
            </article>
          );
        })}
      </div>
    </section>
  );
}
