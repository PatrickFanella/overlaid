import type { ReactNode } from 'react';
import {
  defaultPanelLayouts,
  movePanelLayoutItem,
  togglePanelLayoutItem,
  visiblePanelLayoutItems,
  type DashboardPanelPage,
  type PanelLayoutItem,
} from '../../../lib/dashboardLayout';

type PanelDefinition = {
  id: string;
  node: ReactNode;
};

type CustomizablePanelLayoutProps = {
  page: DashboardPanelPage;
  items: PanelLayoutItem[];
  panels: PanelDefinition[];
  onChange: (items: PanelLayoutItem[]) => void;
  title?: string;
  description?: string;
};

export function CustomizablePanelLayout({
  page,
  items,
  panels,
  onChange,
  title = 'Customize panel layout',
  description = 'Reorder or hide safe panels. Locked safety panels stay fixed outside this list.',
}: CustomizablePanelLayoutProps) {
  const orderedItems = [...items].sort((left, right) => left.order - right.order);
  const panelMap = new Map(panels.map((panel) => [panel.id, panel.node]));
  const visibleItems = visiblePanelLayoutItems(orderedItems).filter((item) => panelMap.has(item.id));

  function resetLayout() {
    onChange(defaultPanelLayouts[page].map((item) => ({ ...item })));
  }

  return (
    <section className="grid gap-3">
      <div className="hud-panel hud-panel--flat border border-zinc-700/50 p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div>
            <p className="hud-label">Workspace layout</p>
            <h2 className="text-lg font-semibold text-zinc-100">{title}</h2>
            <p className="mt-1 max-w-2xl text-sm text-zinc-400">{description}</p>
          </div>
          <button className="hud-button hud-button--small" type="button" onClick={resetLayout}>
            Reset layout
          </button>
        </div>

        <p className="mt-3 text-xs uppercase tracking-[0.2em] text-zinc-500">Locked safety panels stay fixed outside this list.</p>

        <div className="mt-3 grid gap-2 md:grid-cols-2">
          {orderedItems.map((item, index) => (
            <article key={item.id} className="hud-compact-card flex items-center justify-between gap-2 border border-zinc-800/80 bg-black/10">
              <div>
                <p className="text-sm font-semibold text-zinc-100">{item.title}</p>
                <p className="text-xs uppercase tracking-[0.18em] text-zinc-500">{item.visible ? 'visible' : 'hidden'}</p>
              </div>
              <div className="flex flex-wrap justify-end gap-1">
                <button
                  aria-label={`Move ${item.title} up`}
                  className="hud-button hud-button--small"
                  disabled={index === 0}
                  type="button"
                  onClick={() => onChange(movePanelLayoutItem(orderedItems, item.id, 'up'))}
                >
                  Up
                </button>
                <button
                  aria-label={`Move ${item.title} down`}
                  className="hud-button hud-button--small"
                  disabled={index === orderedItems.length - 1}
                  type="button"
                  onClick={() => onChange(movePanelLayoutItem(orderedItems, item.id, 'down'))}
                >
                  Down
                </button>
                <button
                  aria-label={`${item.visible ? 'Hide' : 'Show'} ${item.title}`}
                  className="hud-button hud-button--small"
                  type="button"
                  onClick={() => onChange(togglePanelLayoutItem(orderedItems, item.id))}
                >
                  {item.visible ? 'Hide' : 'Show'}
                </button>
              </div>
            </article>
          ))}
        </div>
      </div>

      {visibleItems.map((item) => (
        <div key={item.id}>{panelMap.get(item.id)}</div>
      ))}
    </section>
  );
}
