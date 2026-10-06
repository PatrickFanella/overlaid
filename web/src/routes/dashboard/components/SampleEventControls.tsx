import { sendTestAlert, setBRBStatus } from '../../../lib/api';
import { sampleAlert, sampleBRBHide, sampleBRBShow, sampleGoals, sampleTicker } from '../../../lib/dashboardSamples';
import { Panel } from '../ui';

type SampleEventControlsProps = {
  apiBaseUrl: string;
  operatorSecret: string;
  isRunning: boolean;
  compact?: boolean;
  runAction: (label: string, action: () => Promise<void>) => void;
};

export function SampleEventControls({ apiBaseUrl, operatorSecret, isRunning, compact = false, runAction }: SampleEventControlsProps) {
  return (
    <Panel kicker="Sample events" title="Send test payloads" description={compact ? undefined : 'Send sample alert, ticker, goals, and BRB payloads to the overlays.'}>
      <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
        <button className="desk-btn desk-btn--tile flex-col items-start" type="button" disabled={isRunning} onClick={() => runAction('Sample alert', () => sendTestAlert(sampleAlert, apiBaseUrl, operatorSecret))}>
          <span className="desk-panel__kicker">Alert</span>
          <span>Sample alert</span>
        </button>

        <button className="desk-btn desk-btn--tile flex-col items-start" type="button" disabled={isRunning} onClick={() => runAction('Sample ticker', () => sendTestAlert(sampleTicker, apiBaseUrl, operatorSecret))}>
          <span className="desk-panel__kicker">Ticker</span>
          <span>Sample ticker</span>
        </button>

        <button className="desk-btn desk-btn--tile flex-col items-start" type="button" disabled={isRunning} onClick={() => runAction('Sample goals', () => sendTestAlert(sampleGoals, apiBaseUrl, operatorSecret))}>
          <span className="desk-panel__kicker">Goals</span>
          <span>Sample goals</span>
        </button>

        <button className="desk-btn desk-btn--tile flex-col items-start" type="button" disabled={isRunning} onClick={() => runAction('Show BRB sample', () => setBRBStatus(sampleBRBShow, apiBaseUrl, operatorSecret))}>
          <span className="desk-panel__kicker">BRB</span>
          <span>Show BRB</span>
        </button>

        <button className="desk-btn desk-btn--tile flex-col items-start" type="button" disabled={isRunning} onClick={() => runAction('Hide BRB sample', () => setBRBStatus(sampleBRBHide, apiBaseUrl, operatorSecret))}>
          <span className="desk-panel__kicker">BRB</span>
          <span>Hide BRB</span>
        </button>
      </div>
    </Panel>
  );
}
