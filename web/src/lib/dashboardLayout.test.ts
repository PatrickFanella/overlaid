import { describe, expect, it } from 'vitest';
import { mergePanelLayoutSubset, movePanelLayoutItem, togglePanelLayoutItem, type PanelLayoutItem } from './dashboardLayout';

describe('partial route layout changes', () => {
  const items: PanelLayoutItem[] = [
    { id: 'one', title: 'One', visible: true, order: 1 },
    { id: 'elsewhere', title: 'Elsewhere', visible: false, order: 2 },
    { id: 'three', title: 'Three', visible: false, order: 3 },
  ];

  it('reorders edited panels in their original slots without collisions or changes elsewhere', () => {
    const edited = movePanelLayoutItem(items.filter((item) => item.id !== 'elsewhere'), 'three', 'up');
    const merged = mergePanelLayoutSubset(items, edited);
    expect(merged.map((item) => [item.id, item.order, item.visible])).toEqual([
      ['three', 1, false], ['elsewhere', 2, false], ['one', 3, true],
    ]);
    expect(items.map((item) => item.id)).toEqual(['one', 'elsewhere', 'three']);
  });

  it('resets only edited panels and ignores unknown or repeated definitions', () => {
    const reset = [{ ...items[0], visible: true }, { ...items[2], visible: true }];
    const merged = mergePanelLayoutSubset(items, [...reset, reset[0], { ...items[0], id: 'unknown' }]);
    expect(merged).toEqual([items[0], items[1], { ...items[2], visible: true }]);
    expect(mergePanelLayoutSubset(items, [])).toEqual(items);
    expect(mergePanelLayoutSubset(items, togglePanelLayoutItem([items[2]], 'three'))[2].visible).toBe(true);
  });
});
