import { useState } from 'react';
import { getConfirmableActionLabel, nextConfirmableActionState } from '../../../lib/controlActions';

export function ConfirmableActionButton({ label, confirm, disabled, disabledReason, onRun }: { label: string; confirm?: boolean; disabled?: boolean; disabledReason?: string; onRun: () => void }) {
  const [armed, setArmed] = useState(false);

  return (
    <button
      className={`hud-action-button hud-action-button--compact ${armed ? 'hud-accent-rose' : ''}`}
      type="button"
      disabled={disabled}
      title={disabledReason || undefined}
      onClick={() => {
        const next = nextConfirmableActionState({ confirm, armed });
        setArmed(next.armed);
        if (next.shouldRun) onRun();
      }}
    >
      {getConfirmableActionLabel(label, armed)}
    </button>
  );
}
