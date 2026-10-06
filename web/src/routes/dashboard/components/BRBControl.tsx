import { useState } from 'react';
import { setBRBStatus } from '../../../lib/api';
import { buildHideBRBPayload, buildShowBRBPayload } from '../../../lib/brbPayloads';
import { sampleBRBShow } from '../../../lib/dashboardSamples';
import { Field, Panel, Tag } from '../ui';

type BRBControlProps = {
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  compact?: boolean;
  runAction: (label: string, action: () => Promise<void>) => void;
};

export function BRBControl({ apiBaseUrl, operatorSecret, isRunning, compact = false, runAction }: BRBControlProps) {
  const [active, setActive] = useState(false);
  const [title, setTitle] = useState(sampleBRBShow.title);
  const [message, setMessage] = useState(sampleBRBShow.message);
  const [eta, setEta] = useState(sampleBRBShow.eta);
  const [itemsText, setItemsText] = useState(sampleBRBShow.items.join('\n'));

  function submitBRB(nextActive: boolean) {
    const payload = nextActive ? buildShowBRBPayload({ title, message, eta, itemsText }) : buildHideBRBPayload();

    runAction(nextActive ? 'Show BRB' : 'Hide BRB', async () => {
      await setBRBStatus(payload, apiBaseUrl, operatorSecret);
      setActive(nextActive);
    });
  }

  return (
    <Panel
      kicker="BRB overlay"
      title="Be right back"
      description={compact ? undefined : 'Sets the copy on /overlay/brb. Switch OBS to the BRB scene separately or with a quick action.'}
      actions={<Tag tone={active ? 'warn' : 'neutral'}>{active ? 'showing' : 'hidden'}</Tag>}
    >
      <div className="grid gap-3 md:grid-cols-[1fr_8rem]">
        <Field label="Title">
          <input className="desk-input" type="text" value={title} onChange={(event) => setTitle(event.target.value)} />
        </Field>
        <Field label="ETA">
          <input className="desk-input" type="text" value={eta} onChange={(event) => setEta(event.target.value)} />
        </Field>
      </div>
      <Field label="Message">
        <input className="desk-input" type="text" value={message} onChange={(event) => setMessage(event.target.value)} />
      </Field>
      {compact ? null : (
        <Field label="Items, one per line">
          <textarea className="desk-input" value={itemsText} onChange={(event) => setItemsText(event.target.value)} />
        </Field>
      )}

      <div className="flex flex-wrap gap-2">
        <button className="desk-btn desk-btn--primary" type="button" disabled={isRunning} onClick={() => submitBRB(true)}>
          Show BRB
        </button>
        <button className="desk-btn" type="button" disabled={isRunning} onClick={() => submitBRB(false)}>
          Hide BRB
        </button>
      </div>
    </Panel>
  );
}
