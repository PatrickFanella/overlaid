import { useState } from 'react';
import type { DashboardConfig } from '../../../lib/dashboardConfig';
import { Check, Notice, Panel } from '../ui';

type SetupChecklistProps = {
  config: DashboardConfig;
  onChange: (config: DashboardConfig) => void;
};

export function SetupChecklist({ config, onChange }: SetupChecklistProps) {
  const [showCompleted, setShowCompleted] = useState(false);
  const allComplete = config.setupChecklist.every((item) => item.completed);
  const visibleItems = config.setupChecklist.filter((item) => !item.completed || showCompleted);

  function toggle(id: string) {
    onChange({
      ...config,
      setupChecklist: config.setupChecklist.map((item) => (item.id === id ? { ...item, completed: !item.completed } : item)),
    });
  }

  return (
    <Panel
      kicker="OBS setup workflow"
      title="Checklist"
      description="Track OBS onboarding, smoke tests, and setup decisions locally."
      flush
      actions={
        <button className="desk-btn desk-btn--small desk-btn--ghost" type="button" onClick={() => setShowCompleted((current) => !current)}>
          {showCompleted ? 'Hide completed' : 'Show completed'}
        </button>
      }
    >
      {allComplete ? (
        <div className="p-4 pb-0">
          <Notice tone="ok" role="status">
            Setup complete. Every workflow item is checked.
          </Notice>
        </div>
      ) : null}

      <ul className="desk-rows">
        {visibleItems.map((item) => (
          <li key={item.id} className={`desk-row ${item.completed ? 'desk-row--ok' : ''}`}>
            <Check label={<span className="desk-row__title">{item.title}</span>} checked={item.completed} onChange={() => toggle(item.id)} />
            <p className="desk-row__text pl-[23px]">{item.description}</p>
          </li>
        ))}
      </ul>
    </Panel>
  );
}
