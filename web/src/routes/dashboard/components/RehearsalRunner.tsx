import { useState } from 'react';
import { createRehearsalSteps } from '../../../lib/rehearsal';

type StepState = 'pending' | 'running' | 'pass' | 'fail';

export function RehearsalRunner({ apiBaseUrl, operatorSecret }: { apiBaseUrl: string; operatorSecret: string }) {
  const steps = createRehearsalSteps(apiBaseUrl, operatorSecret);
  const [states, setStates] = useState<Record<string, StepState>>({});
  const [running, setRunning] = useState(false);

  async function runAll() {
    setRunning(true);
    const next: Record<string, StepState> = {};
    for (const step of steps) {
      next[step.id] = 'running';
      setStates({ ...next });
      try {
        await step.run();
        next[step.id] = 'pass';
      } catch {
        next[step.id] = 'fail';
        setStates({ ...next });
        setRunning(false);
        return;
      }
      setStates({ ...next });
    }
    setRunning(false);
  }

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex items-center justify-between gap-2">
        <div>
          <p className="hud-label">Rehearsal</p>
          <h2 className="hud-section-title">Run overlay smoke sequence</h2>
        </div>
        <button className="hud-action-button hud-action-button--compact" type="button" disabled={running} onClick={() => void runAll()}>
          {running ? 'Running' : 'Run rehearsal'}
        </button>
      </div>
      <div className="mt-3 grid gap-2 md:grid-cols-5">
        {steps.map((step) => (
          <span key={step.id} className="hud-chip hud-chip--square">
            {step.label}: {states[step.id] || 'pending'}
          </span>
        ))}
      </div>
    </section>
  );
}
