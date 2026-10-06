import { useState } from 'react';
import { getConfirmableActionLabel, nextConfirmableActionState } from '../../../lib/controlActions';

export function ConfirmableActionButton({
  label,
  confirm,
  disabled,
  disabledReason,
  onRun,
  className = 'desk-btn desk-btn--tile',
}: {
  label: string;
  confirm?: boolean;
  disabled?: boolean;
  disabledReason?: string;
  onRun: () => void;
  className?: string;
}) {
  const [armed, setArmed] = useState(false);

  return (
    <button
      className={`${className} ${armed ? 'desk-btn--armed' : ''}`}
      type="button"
      disabled={disabled}
      title={disabledReason || undefined}
      onBlur={() => setArmed(false)}
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
