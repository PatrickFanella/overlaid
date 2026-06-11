import { useState } from 'react';
import { setBRBStatus } from '../../../lib/api';
import { sampleBRBHide, sampleBRBShow } from '../../../lib/dashboardSamples';

type BRBControlProps = {
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  runAction: (label: string, action: () => Promise<void>) => void;
};

export function BRBControl({ apiBaseUrl, operatorSecret, isRunning, runAction }: BRBControlProps) {
  const [active, setActive] = useState(true);
  const [title, setTitle] = useState(sampleBRBShow.title);
  const [message, setMessage] = useState(sampleBRBShow.message);
  const [eta, setEta] = useState(sampleBRBShow.eta);
  const [itemsText, setItemsText] = useState(sampleBRBShow.items.join('\n'));

  function parseItems(value: string) {
    return value
      .split('\n')
      .map((item) => item.trim())
      .filter(Boolean);
  }

  function submitBRB(nextActive = active) {
    const payload = nextActive
      ? { active: true, title: title.trim() || sampleBRBShow.title, message: message.trim() || sampleBRBShow.message, eta: eta.trim(), items: parseItems(itemsText) }
      : { ...sampleBRBHide, active: false };

    runAction(nextActive ? 'Show BRB' : 'Hide BRB', async () => {
      await setBRBStatus(payload, apiBaseUrl, operatorSecret);
      setActive(nextActive);
    });
  }

  return (
    <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
      <div className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
        <div>
          <p className="hud-label">BRB overlay</p>
          <h2 className="mt-3 text-2xl font-black text-white">Be right back</h2>
          <p className="hud-copy mt-2 text-sm text-zinc-300">Push the intermission overlay live or hide it again with the operator secret already loaded in this tab.</p>
        </div>
        <span className={`hud-chip ${active ? 'border-emerald-300/40 text-emerald-100' : 'border-cyan-200/40 text-cyan-100'}`}>{active ? 'active' : 'hidden'}</span>
      </div>

      <div className="mt-5 grid gap-4 lg:grid-cols-[1.1fr_0.9fr]">
        <div className="grid gap-4">
          <label className="grid gap-2 text-sm text-zinc-300">
            <span className="hud-kicker">Title</span>
            <input
              className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-rose-300/30 focus:ring-4"
              type="text"
              value={title}
              onChange={(event) => setTitle(event.target.value)}
            />
          </label>

          <label className="grid gap-2 text-sm text-zinc-300">
            <span className="hud-kicker">Message</span>
            <textarea
              className="min-h-28 rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-rose-300/30 focus:ring-4"
              value={message}
              onChange={(event) => setMessage(event.target.value)}
            />
          </label>
        </div>

        <div className="grid gap-4">
          <label className="grid gap-2 text-sm text-zinc-300">
            <span className="hud-kicker">ETA</span>
            <input
              className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-rose-300/30 focus:ring-4"
              type="text"
              value={eta}
              onChange={(event) => setEta(event.target.value)}
            />
          </label>

          <label className="grid gap-2 text-sm text-zinc-300">
            <span className="hud-kicker">Items, one per line</span>
            <textarea
              className="min-h-28 rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-rose-300/30 focus:ring-4"
              value={itemsText}
              onChange={(event) => setItemsText(event.target.value)}
            />
          </label>
        </div>
      </div>

      <div className="mt-5 flex flex-wrap gap-3">
        <button className="hud-chip" type="button" disabled={isRunning} onClick={() => submitBRB(true)}>
          Show BRB
        </button>
        <button className="hud-chip" type="button" disabled={isRunning} onClick={() => submitBRB(false)}>
          Hide BRB
        </button>
      </div>
    </section>
  );
}
