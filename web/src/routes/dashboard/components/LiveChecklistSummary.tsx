import type { SetupChecklistItem } from '../../../lib/dashboardConfig';
import { Panel, Tag } from '../ui';

type LiveChecklistSummaryProps = {
  items: SetupChecklistItem[];
};

export function LiveChecklistSummary({ items }: LiveChecklistSummaryProps) {
  const completed = items.filter((item) => item.completed).length;
  const total = items.length;

  return (
    <Panel
      kicker="Checklist"
      title="Setup checklist summary"
      description={
        <>
          Read-only. Edit it on <a href="/dashboard/overlays">Overlays</a>.
        </>
      }
      actions={<Tag tone={completed === total ? 'ok' : 'warn'}>{`${completed}/${total} done`}</Tag>}
      flush
    >
      <ul className="desk-rows">
        {items.map((item) => (
          <li key={item.id} className={`desk-row ${item.completed ? 'desk-row--ok' : ''}`}>
            <div className="desk-row__line">
              <p className="desk-row__title">{item.title}</p>
              <span className="desk-row__meta mono">{item.completed ? 'done' : 'open'}</span>
            </div>
          </li>
        ))}
      </ul>
    </Panel>
  );
}
