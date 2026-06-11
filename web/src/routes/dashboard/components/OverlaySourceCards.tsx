import { buildOverlaySourceUrl, overlayRoutes } from '../../../lib/overlayRoutes';

type OverlaySourceCardsProps = {
  apiBaseUrl: string;
  onCopy: (label: string, value: string) => void;
};

export function OverlaySourceCards({ apiBaseUrl, onCopy }: OverlaySourceCardsProps) {
  return (
    <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
      <p className="hud-label">Overlay source URLs</p>
      <h2 className="mt-3 text-2xl font-black text-white">Copy into OBS</h2>
      <p className="hud-copy mt-2 text-sm text-zinc-300">Use these browser-source URLs for each overlay route. Copy buttons stay safe in SSR and without clipboard support.</p>

      <div className="mt-5 grid gap-3 xl:grid-cols-2">
        {overlayRoutes.map((route) => {
          const url = buildOverlaySourceUrl(route.path, apiBaseUrl);

          return (
            <article key={route.path} className="hud-panel hud-corners rounded-[1.2rem] p-4">
              <div className="flex flex-col gap-4">
                <div className="flex flex-col gap-2 md:flex-row md:items-start md:justify-between">
                  <div>
                    <p className="hud-kicker">{route.defaultScene} scene</p>
                    <h3 className="mt-2 text-lg font-semibold text-white">{route.title}</h3>
                    <p className="mt-1 text-sm text-zinc-300">{route.description}</p>
                  </div>
                  <button className="hud-chip whitespace-nowrap" type="button" onClick={() => onCopy(route.title, url)}>
                    Copy URL
                  </button>
                </div>

                <code className="hud-mono break-all rounded-2xl border border-white/10 bg-black/30 px-3 py-2 text-sm text-cyan-100">{url}</code>
                <p className="hud-kicker">Path {route.path}</p>
              </div>
            </article>
          );
        })}
      </div>
    </section>
  );
}
