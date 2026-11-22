// Popup auto-resize through the iframe wrapper (omega-target-chromium-extension/overlay/popup-iframe.html):
// geometry is behaviour, and the cut-over keeps a resizing popup on Chromium and a directly-loaded
// document on Firefox (packages/schema/presentation.json#/parity/rows). The popup document is the
// iframe's target, so the height the content needs has to reach the wrapper — a postMessage to the
// parent with the measured offset height, sent whenever the content resizes.

export const RESIZE_MESSAGE = 'nexora-popup-resize';

/** Observe the popup's content height and report it to the iframe wrapper. Returns the disconnect
 * function. On Firefox the popup is a directly-loaded document (no wrapper), detected by being its own
 * top frame — there, no message is posted and the browser sizes the window itself. */
export function autoResize(target: HTMLElement, post = (message: string) => window.parent.postMessage(message, '*')): () => void {
  if (window.self === window.top) return () => {};
  const report = (): void => {
    post(JSON.stringify({ type: RESIZE_MESSAGE, height: target.offsetHeight }));
  };
  const observer = new ResizeObserver(report);
  observer.observe(target);
  report();
  return () => observer.disconnect();
}
