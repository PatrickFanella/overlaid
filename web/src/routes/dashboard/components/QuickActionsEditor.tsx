import { createQuickAction, quickActionKinds, type DashboardConfig, type QuickAction } from '../../../lib/dashboardConfig';
import { resolveQuickActionSequence } from '../../../lib/quickActionSequences';
import { Check, Field, Notice, Panel, SubPanel, Tag } from '../ui';

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
    <Panel
      kicker="Editable quick actions"
      title="Quick action presets"
      description={compact ? undefined : 'Edit labels and payload fields locally.'}
      actions={<Tag>{config.quickActions.length} actions</Tag>}
    >
      <Field label="Add action" className="max-w-56">
        <select
          className="desk-input"
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
      </Field>

      <div className="grid gap-3">
        {config.quickActions.map((action, index) => (
          <SubPanel key={action.id} title={action.label || 'Untitled action'}>
            <div className="grid gap-3 lg:grid-cols-2">
              <Field label="Label">
                <input className="desk-input" type="text" value={action.label} onChange={(event) => updateAction(index, { label: event.target.value })} />
              </Field>

              <Field label="Kind">
                <select className="desk-input" value={action.kind} onChange={(event) => replaceAction(index, event.target.value as QuickAction['kind'])}>
                  {quickActionKinds.map((kind) => (
                    <option key={kind} value={kind}>{kind}</option>
                  ))}
                </select>
              </Field>

              {action.kind === 'setScene' ? (
                <Field label="Scene name">
                  <input className="desk-input" type="text" value={action.sceneName} onChange={(event) => updateAction(index, { sceneName: event.target.value })} />
                </Field>
              ) : null}

              {action.kind === 'runPreset' ? (
                <>
                  <Field label="Run preset">
                    <input className="desk-input" type="text" value={action.presetId} onChange={(event) => updateAction(index, { presetId: event.target.value })} />
                  </Field>
                  <Field label="Scene name">
                    <input className="desk-input" type="text" value={action.sceneName} onChange={(event) => updateAction(index, { sceneName: event.target.value })} />
                  </Field>
                </>
              ) : null}

              {action.kind === 'sequence' ? (() => {
                const resolvedSequence = resolveQuickActionSequence(action, config.quickActions);
                const selfReferences = action.actionIds
                  .map((actionId) => actionId.trim())
                  .filter((actionId) => actionId && actionId === action.id)
                  .map((actionId) => `Self reference skipped: ${actionId}`);
                const warnings = [...resolvedSequence.warnings, ...selfReferences];

                return (
                  <>
                    <Field label="Action IDs" className="lg:col-span-2" hint="One action ID per line. Sequence actions skip missing, nested, and self references.">
                      <textarea
                        className="desk-input desk-input--mono"
                        value={action.actionIds.join('\n')}
                        onChange={(event) =>
                          updateAction(index, {
                            actionIds: event.target.value
                              .split(/\r?\n/)
                              .map((actionId) => actionId.trim())
                              .filter((actionId) => actionId.length > 0),
                          })
                        }
                      />
                    </Field>

                    <Check className="lg:col-span-2" label="Stop sequence on first failed step" checked={Boolean(action.stopOnError)} onChange={(checked) => updateAction(index, { stopOnError: checked })} />

                    {warnings.length > 0 ? (
                      <div className="lg:col-span-2">
                        <Notice tone="warn">
                          <ul className="grid gap-1">
                            {warnings.map((warning) => (
                              <li key={warning}>{warning}</li>
                            ))}
                          </ul>
                        </Notice>
                      </div>
                    ) : null}
                  </>
                );
              })() : null}

              {action.kind === 'refreshSource' ? (
                <Field label="Source input name">
                  <input className="desk-input" type="text" value={action.inputName} onChange={(event) => updateAction(index, { inputName: event.target.value })} />
                </Field>
              ) : null}

              {action.kind === 'muteInput' ? (
                <Field label="Input name">
                  <input className="desk-input" type="text" value={action.inputName} onChange={(event) => updateAction(index, { inputName: event.target.value })} />
                </Field>
              ) : null}

              {action.kind === 'sendAlert' ? (
                <>
                  <Field label="Alert title">
                    <input className="desk-input" type="text" value={action.title} onChange={(event) => updateAction(index, { title: event.target.value })} />
                  </Field>
                  <Field label="Alert message" className="lg:col-span-2">
                    <textarea className="desk-input" value={action.message} onChange={(event) => updateAction(index, { message: event.target.value })} />
                  </Field>
                </>
              ) : null}

              {action.kind === 'showBRB' ? (
                <>
                  <Field label="BRB title">
                    <input className="desk-input" type="text" value={action.title} onChange={(event) => updateAction(index, { title: event.target.value })} />
                  </Field>
                  <Field label="ETA">
                    <input className="desk-input" type="text" value={action.eta} onChange={(event) => updateAction(index, { eta: event.target.value })} />
                  </Field>
                  <Field label="BRB message" className="lg:col-span-2">
                    <textarea className="desk-input" value={action.message} onChange={(event) => updateAction(index, { message: event.target.value })} />
                  </Field>
                </>
              ) : null}

              {action.kind === 'muteInput' ? (
                <Check label={action.muted ? 'Muted on click' : 'Unmuted on click'} checked={action.muted} onChange={(checked) => updateAction(index, { muted: checked })} />
              ) : null}

              {action.kind === 'hideBRB' || action.kind === 'clearOverlays' ? (
                <p className="desk-field__hint lg:col-span-2">This action sends a fixed payload. Only the label is editable.</p>
              ) : null}
            </div>

            <div className="flex flex-wrap items-center justify-between gap-2">
              <Check label="require confirm" checked={Boolean(action.confirm)} onChange={(checked) => updateAction(index, { confirm: checked })} />
              <button className="desk-btn desk-btn--small desk-btn--danger" type="button" onClick={() => removeAction(index)}>
                Remove
              </button>
            </div>
          </SubPanel>
        ))}
      </div>
    </Panel>
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
    return { ...next, sceneName: next.sceneName || '40_MAIN' };
  }

  if (next.kind === 'sequence') {
    return {
      ...next,
      actionIds: Array.isArray(next.actionIds) ? next.actionIds : ['main-scene', 'refresh-alerts'],
      stopOnError: Boolean(next.stopOnError),
      confirm: Boolean(next.confirm),
    };
  }

  if (next.kind === 'runPreset') {
    return {
      ...next,
      presetId: next.presetId || 'main',
      sceneName: next.sceneName || '40_MAIN',
      confirm: Boolean(next.confirm),
    };
  }

  if (next.kind === 'refreshSource') {
    return { ...next, inputName: next.inputName || 'ALERTS' };
  }

  if (next.kind === 'muteInput') {
    return { ...next, inputName: next.inputName || 'Microphone', muted: Boolean(next.muted) };
  }

  return next;
}
