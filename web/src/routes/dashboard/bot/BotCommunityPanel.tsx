import type { BotCounter, BotCountersResponse, BotQuote, BotQuotesResponse } from '../../../lib/api';
import type { CounterDraft, QuoteDraft } from './types';
import { Empty, Field, Panel, SubPanel } from '../ui';

export function BotCommunityPanel({
  counters,
  quotes,
  counterDraft,
  quoteDraft,
  onCounterDraftChange,
  onQuoteDraftChange,
  onSaveCounter,
  onDeleteCounter,
  onSaveQuote,
  onDeleteQuote,
}: {
  counters: BotCountersResponse | null;
  quotes: BotQuotesResponse | null;
  counterDraft: CounterDraft;
  quoteDraft: QuoteDraft;
  onCounterDraftChange: (draft: CounterDraft) => void;
  onQuoteDraftChange: (draft: QuoteDraft) => void;
  onSaveCounter: () => void;
  onDeleteCounter: (counter: BotCounter) => void;
  onSaveQuote: () => void;
  onDeleteQuote: (quote: BotQuote) => void;
}) {
  const counterItems = counters?.counters ?? [];
  const quoteItems = quotes?.quotes ?? [];
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
          <div className="grid gap-3 md:grid-cols-[1fr_7rem]">
            <Field label="Counter name">
              <input className="desk-input desk-input--mono" value={counterDraft.name} onChange={(event) => onCounterDraftChange({ ...counterDraft, name: event.target.value })} placeholder="hype" />
            </Field>
            <Field label="Value">
              <input className="desk-input" type="number" value={counterDraft.value} onChange={(event) => onCounterDraftChange({ ...counterDraft, value: Number(event.target.value) })} />
            </Field>
          </div>
          <div>
            <button className="desk-btn desk-btn--primary" type="button" disabled={counterDraft.name.trim() === ''} onClick={onSaveCounter}>
              Save counter
            </button>
          </div>
          {counterItems.length === 0 ? (
            <Empty>No counters yet.</Empty>
          ) : (
            <ul className="desk-rows">
              {counterItems.map((counter) => (
                <li key={counter.id} className="desk-row">
                  <div className="desk-row__line">
                    <p className="desk-row__title">
                      <span className="mono">{counter.name}</span> · {counter.value}
                    </p>
                    <button className="desk-btn desk-btn--small desk-btn--danger" type="button" onClick={() => onDeleteCounter(counter)}>
                      Delete
                    </button>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </SubPanel>

        <SubPanel title="Quotes">
          <Field label="Author" hint="Optional.">
            <input className="desk-input" value={quoteDraft.author} onChange={(event) => onQuoteDraftChange({ ...quoteDraft, author: event.target.value })} placeholder="author optional" />
          </Field>
          <Field label="Quote text">
            <textarea className="desk-input" value={quoteDraft.text} onChange={(event) => onQuoteDraftChange({ ...quoteDraft, text: event.target.value })} placeholder="quote text" />
          </Field>
          <div>
            <button className="desk-btn desk-btn--primary" type="button" disabled={quoteDraft.text.trim() === ''} onClick={onSaveQuote}>
              Save quote
            </button>
          </div>
          {quoteItems.length === 0 ? (
            <Empty>No quotes yet.</Empty>
          ) : (
            <ul className="desk-rows">
              {quoteItems.map((quote) => (
                <li key={quote.id} className="desk-row">
                  <p className="desk-row__text">{quote.text}</p>
                  <div className="desk-row__line">
                    <span className="desk-row__meta">{quote.author || 'unknown'}</span>
                    <button className="desk-btn desk-btn--small desk-btn--danger" type="button" onClick={() => onDeleteQuote(quote)}>
                      Delete
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

