import type { BotCommunityState } from './useBotCommunity';
import { Empty, Field, Notice, Panel, SubPanel } from '../ui';

export function BotCommunityPanel({ counters, quotes }: BotCommunityState) {
  const counterItems = counters.collection?.items ?? [];
  const quoteItems = quotes.collection?.items ?? [];
  const counterDraft = counters.draft;
  const quoteDraft = quotes.draft;
  const counterBusy = counters.loading || Boolean(counters.pending);
  const quoteBusy = quotes.loading || Boolean(quotes.pending);
  return (
    <Panel
      id="bot-community"
      kicker="Community"
      title="Bot counters and quotes"
      description={
        <>
          Community tools for <code>!count name</code>, <code>!quote</code>, and moderator <code>!quote add text</code>.
        </>
      }
    >
      <div className="grid gap-4 lg:grid-cols-2">
        <SubPanel title="Counters">
          {counters.loading ? <p className="desk-note">Loading counters…</p> : null}
          {counters.error ? <Notice tone="bad" role="alert">{counters.error}</Notice> : null}
          {counters.result ? <Notice tone="ok" role="status">{counters.result}</Notice> : null}
          {counters.collection?.loadError ? <Notice tone="warn">Counters unavailable: {counters.collection.loadError}</Notice> : null}
          <div className="grid gap-3 md:grid-cols-[1fr_7rem]">
            <Field label="Counter name">
              <input className="desk-input desk-input--mono" disabled={counterBusy} value={counterDraft.name} onChange={(event) => counters.onDraftChange({ ...counterDraft, name: event.target.value })} placeholder="hype" />
            </Field>
            <Field label="Value">
              <input className="desk-input" type="number" disabled={counterBusy} value={counterDraft.value} onChange={(event) => counters.onDraftChange({ ...counterDraft, value: Number(event.target.value) })} />
            </Field>
          </div>
          <div>
            <button className="desk-btn desk-btn--primary" type="button" disabled={counterBusy || counterDraft.name.trim() === '' || !Number.isSafeInteger(counterDraft.value)} onClick={counters.onSave}>
              {counters.pending?.action === 'save' ? 'Saving counter…' : 'Save counter'}
            </button>
          </div>
          {!counters.loading && !counters.collection?.loadError && counterItems.length === 0 ? (
            <Empty>No counters yet.</Empty>
          ) : (
            <ul className="desk-rows">
              {counterItems.map((counter) => (
                <li key={counter.id} className="desk-row">
                  <div className="desk-row__line">
                    <p className="desk-row__title">
                      <span className="mono">{counter.name}</span> · {counter.value}
                    </p>
                    <button className="desk-btn desk-btn--small desk-btn--danger" type="button" disabled={counterBusy} onClick={() => counters.onDelete(counter)}>
                      {counters.pending?.action === 'delete' && counters.pending.id === counter.id ? 'Deleting…' : 'Delete'}
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </SubPanel>

        <SubPanel title="Quotes">
          {quotes.loading ? <p className="desk-note">Loading quotes…</p> : null}
          {quotes.error ? <Notice tone="bad" role="alert">{quotes.error}</Notice> : null}
          {quotes.result ? <Notice tone="ok" role="status">{quotes.result}</Notice> : null}
          {quotes.collection?.loadError ? <Notice tone="warn">Quotes unavailable: {quotes.collection.loadError}</Notice> : null}
          <Field label="Author" hint="Optional.">
            <input className="desk-input" disabled={quoteBusy} value={quoteDraft.author} onChange={(event) => quotes.onDraftChange({ ...quoteDraft, author: event.target.value })} placeholder="author optional" />
          </Field>
          <Field label="Quote text">
            <textarea className="desk-input" disabled={quoteBusy} value={quoteDraft.text} onChange={(event) => quotes.onDraftChange({ ...quoteDraft, text: event.target.value })} placeholder="quote text" />
          </Field>
          <div>
            <button className="desk-btn desk-btn--primary" type="button" disabled={quoteBusy || quoteDraft.text.trim() === ''} onClick={quotes.onSave}>
              {quotes.pending?.action === 'save' ? 'Saving quote…' : 'Save quote'}
            </button>
          </div>
          {!quotes.loading && !quotes.collection?.loadError && quoteItems.length === 0 ? (
            <Empty>No quotes yet.</Empty>
          ) : (
            <ul className="desk-rows">
              {quoteItems.map((quote) => (
                <li key={quote.id} className="desk-row">
                  <p className="desk-row__text">{quote.text}</p>
                  <div className="desk-row__line">
                    <span className="desk-row__meta">{quote.author || 'unknown'}</span>
                    <button className="desk-btn desk-btn--small desk-btn--danger" type="button" disabled={quoteBusy} onClick={() => quotes.onDelete(quote)}>
                      {quotes.pending?.action === 'delete' && quotes.pending.id === quote.id ? 'Deleting…' : 'Delete'}
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </SubPanel>
      </div>
    </Panel>
  );
}

