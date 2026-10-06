import type { ReactNode } from 'react';
import {
  defaultPanelLayouts,
  movePanelLayoutItem,
  togglePanelLayoutItem,
  visiblePanelLayoutItems,
  type DashboardPanelPage,
  type PanelLayoutItem,
} from '../../../lib/dashboardLayout';
import { Disclosure } from '../ui';

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
  className?: string;
};

// Arrangeable panels. The editor stays folded away so it never competes with
// the panels themselves; locked safety panels are rendered outside this list.
export function CustomizablePanelLayout({
  page,
  items,
  panels,
  onChange,
  title = 'Customize panel layout',
  description = 'Reorder or hide safe panels. Locked safety panels stay fixed outside this list.',
  className = 'grid gap-4',
}: CustomizablePanelLayoutProps) {
  const panelMap = new Map(panels.map((panel) => [panel.id, panel.node]));
  // A layout can name panels that live on another page; only list the ones here.
  const orderedItems = [...items].sort((left, right) => left.order - right.order).filter((item) => panelMap.has(item.id));
  const visibleItems = visiblePanelLayoutItems(orderedItems);

  function update(next: PanelLayoutItem[]) {
    const nextById = new Map(next.map((item) => [item.id, item]));
    onChange(items.map((item) => nextById.get(item.id) ?? item));
  }

  function resetLayout() {
    onChange(defaultPanelLayouts[page].map((item) => ({ ...item })));
  }

  return (
    <>
      <Disclosure summary={title}>
        <div className="desk-sub">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <p className="desk-row__text">{description}</p>
            <button className="desk-btn desk-btn--small" type="button" onClick={resetLayout}>
              Reset layout
            </button>
          </div>
          <p className="desk-field__hint">Locked safety panels stay fixed outside this list.</p>

          <ol className="grid gap-2 md:grid-cols-2">
            {orderedItems.map((item, index) => (
              <li key={item.id} className="flex items-center justify-between gap-2">
                <span className={item.visible ? '' : 'desk-empty'}>
                  {item.title} <span className="desk-row__meta mono">{item.visible ? '' : 'hidden'}</span>
                </span>
                <span className="flex gap-1">
                  <button aria-label={`Move ${item.title} up`} className="desk-btn desk-btn--small" disabled={index === 0} type="button" onClick={() => update(movePanelLayoutItem(orderedItems, item.id, 'up'))}>
                    ↑
                  </button>
                  <button aria-label={`Move ${item.title} down`} className="desk-btn desk-btn--small" disabled={index === orderedItems.length - 1} type="button" onClick={() => update(movePanelLayoutItem(orderedItems, item.id, 'down'))}>
                    ↓
                  </button>
                  <button aria-label={`${item.visible ? 'Hide' : 'Show'} ${item.title}`} className="desk-btn desk-btn--small" type="button" onClick={() => update(togglePanelLayoutItem(orderedItems, item.id))}>
                    {item.visible ? 'Hide' : 'Show'}
                  </button>
                </span>
              </li>
            ))}
          </ol>
        </div>
      </Disclosure>

      <div className={className}>
        {visibleItems.map((item) => (
          <div key={item.id} className="min-w-0">
            {panelMap.get(item.id)}
          </div>
        ))}
      </div>
    </>
  );
}
