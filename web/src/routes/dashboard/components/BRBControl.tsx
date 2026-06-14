import { useState } from 'react';
import { setBRBStatus } from '../../../lib/api';
import { buildHideBRBPayload, buildShowBRBPayload } from '../../../lib/brbPayloads';
import { sampleBRBShow } from '../../../lib/dashboardSamples';

type BRBControlProps = {
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  compact?: boolean;
  runAction: (label: string, action: () => Promise<void>) => void;
};

export function BRBControl({ apiBaseUrl, operatorSecret, isRunning, compact = false, runAction }: BRBControlProps) {
  const [active, setActive] = useState(true);
  const [title, setTitle] = useState(sampleBRBShow.title);
  const [message, setMessage] = useState(sampleBRBShow.message);
  const [eta, setEta] = useState(sampleBRBShow.eta);
  const [itemsText, setItemsText] = useState(sampleBRBShow.items.join('\n'));

  function submitBRB(nextActive = active) {
    const payload = nextActive ? buildShowBRBPayload({ title, message, eta, itemsText }) : buildHideBRBPayload();

    runAction(nextActive ? 'Show BRB' : 'Hide BRB', async () => {
      await setBRBStatus(payload, apiBaseUrl, operatorSecret);
      setActive(nextActive);
    });
  }

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
        <div>
          <p className="hud-label">BRB overlay</p>
          <h2 className="hud-section-title">Be right back</h2>
          {!compact ? <p className="hud-copy mt-1 text-xs text-zinc-300">Push the intermission overlay live or hide it again.</p> : null}
        </div>
        <span className={`hud-chip hud-chip--square ${active ? 'border-emerald-300/40 text-emerald-100' : 'border-cyan-200/40 text-cyan-100'}`}>{active ? 'active' : 'hidden'}</span>
      </div>

      <div className="mt-3 grid gap-3 lg:grid-cols-[1.1fr_0.9fr]">
        <div className="grid gap-3">
          <label className="grid gap-1 text-xs text-zinc-300">
            <span className="hud-kicker">Title</span>
            <input
              className="hud-input"
              type="text"
              value={title}
              onChange={(event) => setTitle(event.target.value)}
            />
          </label>

          <label className="grid gap-1 text-xs text-zinc-300">
            <span className="hud-kicker">Message</span>
            <textarea
              className="hud-input min-h-20"
              value={message}
              onChange={(event) => setMessage(event.target.value)}
            />
          </label>
        </div>

        <div className="grid gap-3">
          <label className="grid gap-1 text-xs text-zinc-300">
            <span className="hud-kicker">ETA</span>
            <input
              className="hud-input"
              type="text"
              value={eta}
              onChange={(event) => setEta(event.target.value)}
            />
          </label>

          <label className="grid gap-1 text-xs text-zinc-300">
            <span className="hud-kicker">Items, one per line</span>
            <textarea
              className="hud-input min-h-20"
              value={itemsText}
              onChange={(event) => setItemsText(event.target.value)}
            />
          </label>
        </div>
      </div>

      <div className="mt-3 flex flex-wrap gap-2">
        <button className="hud-chip hud-chip--square" type="button" disabled={isRunning} onClick={() => submitBRB(true)}>
          Show BRB
        </button>
        <button className="hud-chip hud-chip--square" type="button" disabled={isRunning} onClick={() => submitBRB(false)}>
          Hide BRB
        </button>
      </div>
    </section>
  );
}
