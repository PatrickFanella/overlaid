import { useEffect, useRef, useState } from 'react';
import { buildOverlaySourceUrl, overlayRoutes, type OverlayRoute } from '../../../lib/overlayRoutes';
import { Panel, Tag } from '../ui';

type OverlayGalleryProps = {
  apiBaseUrl: string;
  onCopy: (label: string, value: string) => void;
};

export function OverlayGallery({ apiBaseUrl, onCopy }: OverlayGalleryProps) {
  return (
    <Panel
      kicker="Overlay source URLs"
      title="Browser sources"
      description="Each preview plays sample events locally (?demo=1). Copy the URL into an OBS Browser Source at the listed size."
    >
      <ul className="grid gap-4 md:grid-cols-2 2xl:grid-cols-3">
        {overlayRoutes.map((route) => {
          const url = buildOverlaySourceUrl(route.path, apiBaseUrl);

          return (
            <li key={route.path} className="desk-panel overflow-hidden">
              <OverlayPreview route={route} />
              <div className="grid gap-2 p-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h3 className="desk-panel__title">{route.title}</h3>
                  <Tag>{route.defaultScene} scene</Tag>
                </div>
                <p className="desk-row__text">{route.description}</p>
                <code className="desk-url">{url}</code>
                <div className="flex flex-wrap gap-2">
                  <button className="desk-btn desk-btn--small desk-btn--primary" type="button" onClick={() => onCopy(route.title, url)}>
                    Copy URL
                  </button>
                  <a className="desk-btn desk-btn--small" href={`${route.path}?demo=1`} target="_blank" rel="noreferrer">
                    Open demo
                  </a>
                  <span className="desk-row__meta mono self-center">Path {route.path}</span>
                </div>
              </div>
            </li>
          );
        })}
      </ul>
    </Panel>
  );
}

// Renders the overlay at its real OBS size and scales it into a 16:9 box.
function OverlayPreview({ route }: { route: OverlayRoute }) {
  const ref = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState({ width: 384, height: 216 });

  useEffect(() => {
    const element = ref.current;
    if (!element || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(([entry]) => {
      setBox({ width: entry.contentRect.width, height: entry.contentRect.height });
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  const { width, height } = route.size;
  const scale = Math.min(box.width / width, box.height / height);
  const left = (box.width - width * scale) / 2;
  const top = (box.height - height * scale) / 2;

  return (
    <div ref={ref} className="desk-preview">
      <iframe
        title={`${route.title} preview`}
        src={`${route.path}?demo=1`}
        width={width}
        height={height}
        loading="lazy"
        tabIndex={-1}
        aria-hidden="true"
        style={{ transform: `translate(${left}px, ${top}px) scale(${scale})` }}
      />
      <span className="desk-preview__size">
        {width}×{height}
      </span>
    </div>
  );
}
