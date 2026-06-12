import { sendTestAlert, setBRBStatus, type AlertPayload } from './api';
import { sampleAlert, sampleBRBHide, sampleBRBShow, sampleGoals, sampleTicker } from './dashboardSamples';

export type RehearsalStep = {
  id: string;
  label: string;
  run: () => Promise<void>;
};

export function createRehearsalSteps(apiBaseUrl: string, operatorSecret: string): RehearsalStep[] {
  const alertStep = (id: string, label: string, payload: AlertPayload): RehearsalStep => ({
    id,
    label,
    run: () => sendTestAlert(payload, apiBaseUrl, operatorSecret),
  });

  return [
    alertStep('alert', 'Sample alert', sampleAlert),
    alertStep('ticker', 'Sample ticker', sampleTicker),
    alertStep('goals', 'Sample goals', sampleGoals),
    { id: 'brb-show', label: 'Show BRB', run: () => setBRBStatus(sampleBRBShow, apiBaseUrl, operatorSecret) },
    { id: 'brb-hide', label: 'Hide BRB', run: () => setBRBStatus(sampleBRBHide, apiBaseUrl, operatorSecret) },
  ];
}
