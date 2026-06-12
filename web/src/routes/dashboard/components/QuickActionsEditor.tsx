import { createQuickAction, quickActionKinds, type DashboardConfig, type QuickAction } from '../../../lib/dashboardConfig';

type QuickActionsEditorProps = {
  config: DashboardConfig;
  onChange: (config: DashboardConfig) => void;
  compact?: boolean;
};

export function QuickActionsEditor({ config, onChange, compact = false }: QuickActionsEditorProps) {
  function updateAction(index: number, patch: Partial<QuickAction>) {
    onChange({
      ...config,
      quickActions: config.quickActions.map((action, currentIndex) => (currentIndex === index ? mergeAction(action, patch) : action)),
    });
  }

  function replaceAction(index: number, kind: QuickAction['kind']) {
    const previous = config.quickActions[index];
    const next = createQuickAction(kind, previous?.label || 'New action');
    onChange({
      ...config,
      quickActions: config.quickActions.map((action, currentIndex) => (currentIndex === index ? { ...next, label: previous?.label || next.label } : action)),
    });
  }

  function addAction(kind: QuickAction['kind']) {
    onChange({
      ...config,
      quickActions: [...config.quickActions, createQuickAction(kind, 'New action')],
    });
  }

  function removeAction(index: number) {
    onChange({
      ...config,
      quickActions: config.quickActions.filter((_, currentIndex) => currentIndex !== index),
    });
  }

  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <div className="flex flex-col gap-2 md:flex-row md:items-end md:justify-between">
        <div>
          <p className="hud-label">Editable quick actions</p>
          <h2 className="hud-section-title">Quick action presets</h2>
          {!compact ? <p className="hud-copy mt-1 text-xs text-zinc-300">Edit labels and payload fields locally.</p> : null}
        </div>
        <span className="hud-chip hud-chip--square">{config.quickActions.length} actions</span>
      </div>

      <div className="mt-3 flex flex-wrap gap-2">
        <select
          className="hud-input max-w-56"
          defaultValue="setScene"
          onChange={(event) => {
            const kind = event.target.value as QuickAction['kind'];
            addAction(kind);
            event.target.value = 'setScene';
          }}
        >
          {quickActionKinds.map((kind) => (
            <option key={kind} value={kind}>{kind}</option>
          ))}
        </select>
      </div>

      <div className="mt-3 grid gap-3">
        {config.quickActions.map((action, index) => (
          <article key={action.id} className="hud-panel hud-corners hud-panel--flat hud-compact-card">
            <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
              <div className="grid gap-3 lg:flex-1 lg:grid-cols-2">
                <label className="grid gap-2 text-sm text-zinc-300">
                  <span className="hud-kicker">Label</span>
                  <input
                    className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4"
                    type="text"
                    value={action.label}
                    onChange={(event) => updateAction(index, { label: event.target.value })}
                  />
                </label>

                <label className="grid gap-2 text-sm text-zinc-300">
                  <span className="hud-kicker">Kind</span>
                  <select
                    className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-sm text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4"
                    value={action.kind}
                    onChange={(event) => replaceAction(index, event.target.value as QuickAction['kind'])}
                  >
                    {quickActionKinds.map((kind) => (
                      <option key={kind} value={kind}>{kind}</option>
                    ))}
                  </select>
                </label>
              </div>

              <button className="hud-chip self-start" type="button" onClick={() => removeAction(index)}>
                Remove
              </button>
            </div>

            <label className="mt-3 flex items-center gap-2 text-xs text-zinc-300">
              <input
                className="size-4 accent-cyan-300"
                type="checkbox"
                checked={Boolean(action.confirm)}
                onChange={(event) => updateAction(index, { confirm: event.target.checked })}
              />
              require confirm
            </label>

            <div className="mt-4 grid gap-3 lg:grid-cols-2">
              {action.kind === 'setScene' ? (
                <label className="grid gap-2 text-sm text-zinc-300">
                  <span className="hud-kicker">Scene name</span>
                  <input
                    className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4"
                    type="text"
                    value={action.sceneName}
                    onChange={(event) => updateAction(index, { sceneName: event.target.value })}
                  />
                </label>
              ) : null}

              {action.kind === 'muteInput' ? (
                <label className="grid gap-2 text-sm text-zinc-300">
                  <span className="hud-kicker">Input name</span>
                  <input
                    className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4"
                    type="text"
                    value={action.inputName}
                    onChange={(event) => updateAction(index, { inputName: event.target.value })}
                  />
                </label>
              ) : null}

              {action.kind === 'sendAlert' ? (
                <>
                  <label className="grid gap-2 text-sm text-zinc-300">
                    <span className="hud-kicker">Alert title</span>
                    <input
                      className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4"
                      type="text"
                      value={action.title}
                      onChange={(event) => updateAction(index, { title: event.target.value })}
                    />
                  </label>
                  <label className="grid gap-2 text-sm text-zinc-300 lg:col-span-2">
                    <span className="hud-kicker">Alert message</span>
                    <textarea
                      className="min-h-24 rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4"
                      value={action.message}
                      onChange={(event) => updateAction(index, { message: event.target.value })}
                    />
                  </label>
                </>
              ) : null}

              {action.kind === 'showBRB' ? (
                <>
                  <label className="grid gap-2 text-sm text-zinc-300">
                    <span className="hud-kicker">BRB title</span>
                    <input
                      className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4"
                      type="text"
                      value={action.title}
                      onChange={(event) => updateAction(index, { title: event.target.value })}
                    />
                  </label>
                  <label className="grid gap-2 text-sm text-zinc-300">
                    <span className="hud-kicker">ETA</span>
                    <input
                      className="rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4"
                      type="text"
                      value={action.eta}
                      onChange={(event) => updateAction(index, { eta: event.target.value })}
                    />
                  </label>
                  <label className="grid gap-2 text-sm text-zinc-300 lg:col-span-2">
                    <span className="hud-kicker">BRB message</span>
                    <textarea
                      className="min-h-24 rounded-2xl border border-white/10 bg-zinc-950 px-4 py-3 text-zinc-100 outline-none ring-cyan-300/30 focus:ring-4"
                      value={action.message}
                      onChange={(event) => updateAction(index, { message: event.target.value })}
                    />
                  </label>
                </>
              ) : null}

              {action.kind === 'muteInput' ? (
                <label className="flex items-center gap-3 rounded-2xl border border-white/10 bg-black/20 px-4 py-3 text-sm text-zinc-300">
                  <input
                    className="size-4 accent-cyan-300"
                    type="checkbox"
                    checked={action.muted}
                    onChange={(event) => updateAction(index, { muted: event.target.checked })}
                  />
                  <span>{action.muted ? 'Muted on click' : 'Unmuted on click'}</span>
                </label>
              ) : null}

              {action.kind === 'hideBRB' || action.kind === 'clearOverlays' ? (
                <p className="text-sm text-zinc-400 lg:col-span-2">This quick action uses a fixed payload and only needs its label edited.</p>
              ) : null}
            </div>
          </article>
        ))}
      </div>
    </section>
  );
}

function mergeAction(action: QuickAction, patch: Partial<QuickAction>): QuickAction {
  const next = { ...action, ...patch } as QuickAction;

  if (next.kind === 'sendAlert') {
    return {
      ...next,
      title: next.title || 'Sample alert',
      message: next.message || 'Quick action fired',
      severity: next.severity || 'success',
    };
  }

  if (next.kind === 'showBRB') {
    return {
      ...next,
      title: next.title || 'Be right back',
      message: next.message || 'Short intermission. Stream resumes soon.',
      eta: next.eta || '5 min',
      items: next.items || ['Hydrate', 'Stretch', 'Chat stays live'],
    };
  }

  if (next.kind === 'setScene') {
    return { ...next, sceneName: next.sceneName || 'Main' };
  }

  if (next.kind === 'muteInput') {
    return { ...next, inputName: next.inputName || 'Microphone', muted: Boolean(next.muted) };
  }

  return next;
}
