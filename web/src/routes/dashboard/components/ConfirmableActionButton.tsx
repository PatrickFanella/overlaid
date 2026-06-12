import { useState } from 'react';

export function ConfirmableActionButton({ label, confirm, disabled, onRun }: { label: string; confirm?: boolean; disabled?: boolean; onRun: () => void }) {
  const [armed, setArmed] = useState(false);

  return (
    <button
      className={`hud-action-button hud-action-button--compact ${armed ? 'hud-accent-rose' : ''}`}
      type="button"
      disabled={disabled}
      onClick={() => {
        if (confirm && !armed) {
          setArmed(true);
          return;
        }
        setArmed(false);
        onRun();
      }}
    >
      {armed ? `Confirm ${label}` : label}
    </button>
  );
}
