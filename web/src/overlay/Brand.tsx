import type { ReactNode } from 'react';
import type { OverlayStreamStatus } from '../lib/eventStream';
import { isOverlayFlagSet } from '../lib/overlayParams';

// SUBCULT poster-press primitives. Marks are the approved files copied from
// studio/brands/subcult; see public/brand/subcult/PROVENANCE.md. Never redraw them.

// `ink` letters sit on purple, green, or bone fields; `white` letters sit on
// ink. The pack names these by field ("dark" = for dark fields), which reads
// backwards in code.
type MarkTone = 'ink' | 'white';

const markFiles: Record<MarkTone, string> = {
  ink: '/brand/subcult/mark-primary.svg',
  white: '/brand/subcult/mark-dark.svg',
};

const lockupFiles: Record<MarkTone, string> = {
  ink: '/brand/subcult/lockup-primary-outlined.svg',
  white: '/brand/subcult/lockup-dark-outlined.svg',
};

// The brand minimum is 32px for the mark and 220px for the lockup.
export function Mark({ tone = 'ink', size = 64, className = '' }: { tone?: MarkTone; size?: number; className?: string }) {
  return <img className={`sc-mark ${className}`} src={markFiles[tone]} width={Math.max(32, size)} height={Math.max(32, size)} alt="SUBCULT" />;
}

export function Lockup({ tone = 'ink', width = 320, className = '' }: { tone?: MarkTone; width?: number; className?: string }) {
  return <img className={`sc-lockup ${className}`} src={lockupFiles[tone]} width={Math.max(220, width)} alt="SUBCULT" />;
}

export function FisheyeMark({ size = 160, className = '' }: { size?: number; className?: string }) {
  return <img className={`sc-fisheye ${className}`} src="/brand/subcult/mark-fisheye-circle.png" width={size} height={size} alt="SUBCULT" />;
}

// Registration crosshair: two hairlines through a point.
export function RegMark({ className = '' }: { className?: string }) {
  return <span aria-hidden="true" className={`sc-reg ${className}`} />;
}

// Text set on a tilted acid-green block, as in the brand's square post.
export function Slab({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <span className={`sc-slab ${className}`}>{children}</span>;
}

export function Kicker({ children, className = '' }: { children: ReactNode; className?: string }) {
  return <p className={`sc-label ${className}`}>{children}</p>;
}

type StageProps = {
  name: string;
  status: OverlayStreamStatus;
  children: ReactNode;
  className?: string;
  label?: string;
};

// Transparent full-viewport root for an OBS browser source. The connection
// pip stays hidden while the stream is live so it never ends up on air;
// `?status=1` pins it on for setup.
export function OverlayStage({ name, status, children, className = '', label }: StageProps) {
  const showPip = status !== 'live' || isOverlayFlagSet('status');

  return (
    <main className={`sc-stage sc-stage--${name} ${className}`} data-stream-status={status} aria-label={label}>
      {children}
      {showPip ? <StreamPip status={status} /> : null}
    </main>
  );
}

function StreamPip({ status }: { status: OverlayStreamStatus }) {
  const text = status === 'live' ? 'live' : status === 'error' ? 'reconnecting' : 'connecting';
  return (
    <p className={`sc-pip sc-pip--${status}`} role="status">
      <span aria-hidden="true" />
      {text}
    </p>
  );
}
