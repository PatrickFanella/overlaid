import type { QuickAction } from './dashboardConfig';

export type OBSValidationIssue = {
  actionId: string;
  label: string;
  field: 'sceneName' | 'inputName';
  value: string;
  message: string;
};

export function validateQuickActionsAgainstOBS(actions: QuickAction[], sceneNames: string[], inputNames: string[]) {
  const sceneSet = new Set(sceneNames);
  const inputSet = new Set(inputNames);
  const issues: OBSValidationIssue[] = [];

  for (const action of actions) {
    if ((action.kind === 'setScene' || action.kind === 'runPreset') && !sceneSet.has(action.sceneName)) {
      issues.push({ actionId: action.id, label: action.label, field: 'sceneName', value: action.sceneName, message: `Missing OBS scene: ${action.sceneName}` });
    }
    if ((action.kind === 'refreshSource' || action.kind === 'muteInput') && !inputSet.has(action.inputName)) {
      issues.push({ actionId: action.id, label: action.label, field: 'inputName', value: action.inputName, message: `Missing OBS input/source: ${action.inputName}` });
    }
  }

  return issues;
}
