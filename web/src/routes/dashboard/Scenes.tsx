import { useState } from 'react';
import { getActiveAutomationProfile, selectQuickActionsForProfile } from '../../lib/automationProfiles';
import { loadDashboardConfig, saveDashboardConfig, type DashboardConfig } from '../../lib/dashboardConfig';
import { ActiveProfileTag, MissingSecretNotice, ProfileWarnings } from './ActiveProfile';
import { BRBControl } from './components/BRBControl';
import { ControlLayoutEditor } from './components/ControlLayoutEditor';
import { OBSControls } from './components/OBSControls';
import { QuickActionRunner } from './components/QuickActionRunner';
import { QuickActionsEditor } from './components/QuickActionsEditor';
import { RehearsalRunner } from './components/RehearsalRunner';
import { SampleEventControls } from './components/SampleEventControls';
import { SessionLogPanel } from './components/SessionLogPanel';
import { DashboardShell } from './DashboardShell';
import { useDeskStatus } from './deskStatus';
import { Disclosure, PageHead } from './ui';
import { useOperatorActions } from './useOperatorActions';

export function DashboardScenesPage() {
  return (
    <DashboardShell activePath="/dashboard/scenes">
      <Scenes />
    </DashboardShell>
  );
}

function Scenes() {
  const { apiBaseUrl, operatorSecret } = useDeskStatus();
  const [config, setConfig] = useState<DashboardConfig>(() => loadDashboardConfig());
  const { sessionLog, runAction, clearLog, isRunning } = useOperatorActions();
  const profile = getActiveAutomationProfile(config);
  const { actions, warnings } = selectQuickActionsForProfile(config);

  function updateConfig(next: DashboardConfig) {
    setConfig(next);
    saveDashboardConfig(next);
  }

  const widgets = [...config.controlWidgets].filter((widget) => widget.enabled).sort((a, b) => a.order - b.order);

  return (
    <>
      <PageHead title="Scenes and actions" lede="Drive OBS directly, edit the quick-action presets that On air uses, and run smoke tests against the overlays." actions={<ActiveProfileTag profile={profile} />} />
      <MissingSecretNotice operatorSecret={operatorSecret} />
      <ProfileWarnings warnings={warnings} />

      <Disclosure summary="Arrange modules">
        <ControlLayoutEditor config={config} onChange={updateConfig} />
      </Disclosure>

      <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
        <div className="grid gap-4">
          {widgets.map((widget) => {
            const compact = widget.compact;
            const shared = { apiBaseUrl, operatorSecret, isRunning, runAction, compact };
            if (widget.id === 'quick-actions') return <QuickActionRunner key={widget.id} actions={actions} actionCatalog={config.quickActions} {...shared} />;
            if (widget.id === 'sample-events') return <SampleEventControls key={widget.id} {...shared} />;
            if (widget.id === 'brb') return <BRBControl key={widget.id} {...shared} />;
            if (widget.id === 'obs') return <OBSControls key={widget.id} {...shared} />;
            if (widget.id === 'editor')
              return (
                <div key={widget.id} id="quick-action-presets">
                  <QuickActionsEditor config={config} onChange={updateConfig} compact={compact} />
                </div>
              );
            return null;
          })}
        </div>

        <div className="grid gap-4">
          <RehearsalRunner apiBaseUrl={apiBaseUrl} operatorSecret={operatorSecret} />
          <SessionLogPanel entries={sessionLog} onClear={clearLog} />
        </div>
      </div>
    </>
  );
}
