import { sampleBRBHide, sampleBRBShow } from './dashboardSamples';

export function parseBRBItems(value: string) {
  return value
    .split('\n')
    .map((item) => item.trim())
    .filter(Boolean);
}

export function buildShowBRBPayload({
  title,
  message,
  eta,
  itemsText,
}: {
  title: string;
  message: string;
  eta: string;
  itemsText: string;
}) {
  return {
    active: true,
    title: title.trim() || sampleBRBShow.title,
    message: message.trim() || sampleBRBShow.message,
    eta: eta.trim(),
    items: parseBRBItems(itemsText),
  };
}

export function buildHideBRBPayload() {
  return { ...sampleBRBHide, active: false };
}
