import type { ReactNode } from 'react';

// Operator desk primitives. Styles live in src/styles/desk.css.

export type Tone = 'ok' | 'warn' | 'bad' | 'neutral';

type PanelProps = {
  title: string;
  kicker?: string;
  description?: ReactNode;
  actions?: ReactNode;
  danger?: boolean;
  flush?: boolean;
  id?: string;
  className?: string;
  children?: ReactNode;
};

export function Panel({ title, kicker, description, actions, danger = false, flush = false, id, className = '', children }: PanelProps) {
  return (
    <section id={id} className={`desk-panel ${danger ? 'desk-panel--danger' : ''} ${className}`} aria-label={title}>
      <header className="desk-panel__head">
        <div className="min-w-0">
          {kicker ? <p className="desk-panel__kicker">{kicker}</p> : null}
          <h2 className="desk-panel__title">{title}</h2>
          {description ? <p className="desk-panel__desc">{description}</p> : null}
        </div>
        {actions ? <div className="desk-panel__actions">{actions}</div> : null}
      </header>
      {children !== undefined && children !== null ? <div className={`desk-panel__body ${flush ? 'desk-panel__body--flush' : ''}`}>{children}</div> : null}
    </section>
  );
}

export function SubPanel({ title, children, className = '' }: { title?: string; children: ReactNode; className?: string }) {
  return (
    <div className={`desk-sub ${className}`}>
      {title ? <p className="desk-sub__title">{title}</p> : null}
      {children}
    </div>
  );
}

export function PageHead({ title, lede, actions }: { title: string; lede?: ReactNode; actions?: ReactNode }) {
  return (
    <header className="desk-page__head">
      <div>
        <h1 className="desk-page__title">{title}</h1>
        {lede ? <p className="desk-page__lede">{lede}</p> : null}
      </div>
      {actions ? <div className="desk-panel__actions">{actions}</div> : null}
    </header>
  );
}

export function Field({ label, hint, className = '', children }: { label: string; hint?: ReactNode; className?: string; children: ReactNode }) {
  return (
    <label className={`desk-field ${className}`}>
      <span>{label}</span>
      {children}
      {hint ? <span className="desk-field__hint">{hint}</span> : null}
    </label>
  );
}

export function Check({ label, checked, onChange, className = '' }: { label: ReactNode; checked: boolean; onChange: (checked: boolean) => void; className?: string }) {
  return (
    <label className={`desk-check ${className}`}>
      <input type="checkbox" checked={checked} onChange={(event) => onChange(event.target.checked)} />
      {label}
    </label>
  );
}

export function Tag({ tone = 'neutral', children, title }: { tone?: Tone; children: ReactNode; title?: string }) {
  return (
    <span className={`desk-tag ${tone === 'neutral' ? '' : `desk-tag--${tone}`}`} title={title}>
      {children}
    </span>
  );
}

export function Lamp({ tone = 'neutral', label, value, busy = false }: { tone?: Tone; label: string; value: ReactNode; busy?: boolean }) {
  return (
    <span className={`desk-lamp ${tone === 'neutral' ? '' : `desk-lamp--${tone}`} ${busy ? 'desk-lamp--busy' : ''}`}>
      {label} <b>{value}</b>
    </span>
  );
}

export function Notice({ tone = 'neutral', children, role }: { tone?: Tone; children: ReactNode; role?: 'status' | 'alert' }) {
  return (
    <div className={`desk-notice ${tone === 'neutral' ? '' : `desk-notice--${tone}`}`} role={role}>
      {children}
    </div>
  );
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="desk-empty">{children}</p>;
}

export function KeyValues({ items }: { items: [string, ReactNode][] }) {
  return (
    <dl className="desk-kv">
      {items.map(([key, value]) => (
        <div key={key} className="contents">
          <dt>{key}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}

export function Disclosure({ summary, children, defaultOpen = false }: { summary: string; children: ReactNode; defaultOpen?: boolean }) {
  return (
    <details className="desk-disclosure" open={defaultOpen || undefined}>
      <summary>{summary}</summary>
      {children}
    </details>
  );
}
