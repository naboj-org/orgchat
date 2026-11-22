// This top-level page signs itself in; the cross-site widget only sends a token.
(() => {
  const opener = window.opener;
  const status = document.getElementById('status');
  if (!opener) {
    status.textContent = 'Return to the website and open chat from the widget again.';
    return;
  }

  const timer = setTimeout(() => {
    window.removeEventListener('message', receiveToken);
    window.opener = null;
    status.textContent = 'Opening chat timed out. Close this tab and try again from the widget.';
  }, 25000);

  async function receiveToken(event) {
    if (event.source !== opener || event.origin !== location.origin ||
        event.data?.type !== 'orgchat:popout-token') return;
    const token = event.data.token;
    if (typeof token !== 'string' || !token || token.length > 16384) return;
    window.removeEventListener('message', receiveToken);
    clearTimeout(timer);
    window.opener = null;
    try {
      const response = await fetch('/api/auth/embed', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token }),
        signal: AbortSignal.timeout(15000),
      });
      if (!response.ok) throw new Error(`Sign-in failed (${response.status}).`);
      location.replace('/');
    } catch (cause) {
      status.textContent = `Could not open chat: ${cause instanceof Error ? cause.message : String(cause)} Close this tab and try again from the widget.`;
    }
  }

  window.addEventListener('message', receiveToken);
  opener.postMessage({ type: 'orgchat:popout-ready' }, location.origin);
})();
