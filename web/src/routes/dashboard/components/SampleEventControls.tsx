import { sendTestAlert, setBRBStatus } from '../../../lib/api';
import { sampleAlert, sampleBRBHide, sampleBRBShow, sampleGoals, sampleTicker } from '../../../lib/dashboardSamples';

type SampleEventControlsProps = {
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  runAction: (label: string, action: () => Promise<void>) => void;
};

export function SampleEventControls({ apiBaseUrl, operatorSecret, isRunning, runAction }: SampleEventControlsProps) {
  return (
    <section className="hud-panel hud-corners rounded-[1.6rem] p-5">
      <p className="hud-label">Sample events</p>
      <h2 className="mt-3 text-2xl font-black text-white">Send test payloads</h2>
      <p className="hud-copy mt-2 text-sm text-zinc-300">Smoke test alerts, ticker, goals, and BRB payloads through the existing event endpoints.</p>

      <div className="hud-dashboard-grid hud-dashboard-grid--three mt-5">
        <button className="hud-action-button" type="button" disabled={isRunning} onClick={() => runAction('Sample alert', () => sendTestAlert(sampleAlert, apiBaseUrl, operatorSecret))}>
          <span className="hud-kicker">Alert</span>
          <span className="mt-2 block text-lg font-semibold text-white">Sample alert</span>
        </button>

        <button className="hud-action-button" type="button" disabled={isRunning} onClick={() => runAction('Sample ticker', () => sendTestAlert(sampleTicker, apiBaseUrl, operatorSecret))}>
          <span className="hud-kicker">Ticker</span>
          <span className="mt-2 block text-lg font-semibold text-white">Sample ticker</span>
        </button>

        <button className="hud-action-button" type="button" disabled={isRunning} onClick={() => runAction('Sample goals', () => sendTestAlert(sampleGoals, apiBaseUrl, operatorSecret))}>
          <span className="hud-kicker">Goals</span>
          <span className="mt-2 block text-lg font-semibold text-white">Sample goals</span>
        </button>

        <button className="hud-action-button" type="button" disabled={isRunning} onClick={() => runAction('Show BRB sample', () => setBRBStatus(sampleBRBShow, apiBaseUrl, operatorSecret))}>
          <span className="hud-kicker">BRB</span>
          <span className="mt-2 block text-lg font-semibold text-white">Show BRB</span>
        </button>

        <button className="hud-action-button" type="button" disabled={isRunning} onClick={() => runAction('Hide BRB sample', () => setBRBStatus(sampleBRBHide, apiBaseUrl, operatorSecret))}>
          <span className="hud-kicker">BRB</span>
          <span className="mt-2 block text-lg font-semibold text-white">Hide BRB</span>
        </button>
      </div>
    </section>
  );
}
