import type { AlertPayload, BRBPayload } from './api';

export const sampleAlert: AlertPayload = {
  title: 'Sample alert',
  message: 'Overlay command center smoke test passed',
  severity: 'success',
};

export const sampleTicker: AlertPayload = {
  title: 'Sample ticker',
  message: 'Lower-third ticker is live',
  severity: 'info',
  data: {
    ticker: {
      id: 'sample-ticker',
      label: 'Ticker sample',
      message: 'Lower-third ticker is live',
    },
  },
};

export const sampleGoals: AlertPayload = {
  title: 'Sample goals',
  message: 'Goal rail accepted a live update',
  severity: 'success',
  data: {
    goal: {
      id: 'followers',
      label: 'Follower goal',
      current: 430,
      target: 620,
      unit: 'followers',
      note: 'Sample goal update',
    },
  },
};

export const sampleBRBShow: BRBPayload = {
  active: true,
  title: 'Be right back',
  message: 'Short intermission. Stream resumes soon.',
  eta: '5 min',
  items: ['Hydrate', 'Stretch', 'Chat stays live'],
  data: { preset: 'sample-show' },
};

export const sampleBRBHide: BRBPayload = {
  ...sampleBRBShow,
  active: false,
  data: { preset: 'sample-hide' },
};
