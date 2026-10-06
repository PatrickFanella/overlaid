import { useState } from 'react';
import { createRehearsalSteps } from '../../../lib/rehearsal';
import { Panel, Tag, type Tone } from '../ui';

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
    <Panel
      kicker="Rehearsal"
      title="Run overlay smoke sequence"
      actions={
        <button className="desk-btn desk-btn--primary" type="button" disabled={running} onClick={() => void runAll()}>
          {running ? 'Running' : 'Run rehearsal'}
        </button>
      }
    >
      <ol className="desk-steps">
        {steps.map((step) => {
          const state = states[step.id] || 'pending';
          return (
            <li key={step.id}>
              <Tag tone={toneForStep(state)}>
                {step.label}: {state}
              </Tag>
            </li>
          );
        })}
      </ol>
    </Panel>
  );
}

function toneForStep(state: StepState): Tone {
  if (state === 'pass') return 'ok';
  if (state === 'fail') return 'bad';
  if (state === 'running') return 'warn';
  return 'neutral';
}
