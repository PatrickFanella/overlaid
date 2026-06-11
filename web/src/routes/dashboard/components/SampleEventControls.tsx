import { sendTestAlert, setBRBStatus } from '../../../lib/api';
import { sampleAlert, sampleBRBHide, sampleBRBShow, sampleGoals, sampleTicker } from '../../../lib/dashboardSamples';

type SampleEventControlsProps = {
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  compact?: boolean;
  runAction: (label: string, action: () => Promise<void>) => void;
};

export function SampleEventControls({ apiBaseUrl, operatorSecret, isRunning, compact = false, runAction }: SampleEventControlsProps) {
  return (
    <section className="hud-panel hud-corners hud-panel--flat rounded-sm p-3">
      <p className="hud-label">Sample events</p>
      <h2 className="hud-section-title">Send test payloads</h2>
      {!compact ? <p className="hud-copy mt-1 text-xs text-zinc-300">Smoke test alerts, ticker, goals, and BRB payloads.</p> : null}

      <div className="hud-dashboard-grid hud-dashboard-grid--three mt-5">
        <button className="hud-action-button hud-action-button--compact" type="button" disabled={isRunning} onClick={() => runAction('Sample alert', () => sendTestAlert(sampleAlert, apiBaseUrl, operatorSecret))}>
          <span className="hud-kicker">Alert</span>
          <span className="mt-1 block text-sm font-bold text-white">Sample alert</span>
        </button>

        <button className="hud-action-button hud-action-button--compact" type="button" disabled={isRunning} onClick={() => runAction('Sample ticker', () => sendTestAlert(sampleTicker, apiBaseUrl, operatorSecret))}>
          <span className="hud-kicker">Ticker</span>
          <span className="mt-1 block text-sm font-bold text-white">Sample ticker</span>
        </button>

        <button className="hud-action-button hud-action-button--compact" type="button" disabled={isRunning} onClick={() => runAction('Sample goals', () => sendTestAlert(sampleGoals, apiBaseUrl, operatorSecret))}>
          <span className="hud-kicker">Goals</span>
          <span className="mt-1 block text-sm font-bold text-white">Sample goals</span>
        </button>

        <button className="hud-action-button hud-action-button--compact" type="button" disabled={isRunning} onClick={() => runAction('Show BRB sample', () => setBRBStatus(sampleBRBShow, apiBaseUrl, operatorSecret))}>
          <span className="hud-kicker">BRB</span>
          <span className="mt-1 block text-sm font-bold text-white">Show BRB</span>
        </button>

        <button className="hud-action-button hud-action-button--compact" type="button" disabled={isRunning} onClick={() => runAction('Hide BRB sample', () => setBRBStatus(sampleBRBHide, apiBaseUrl, operatorSecret))}>
          <span className="hud-kicker">BRB</span>
          <span className="mt-1 block text-sm font-bold text-white">Hide BRB</span>
        </button>
      </div>
    </section>
  );
}
