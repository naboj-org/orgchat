// Claim a one-time session handoff without relying on window.opener.
(async () => {
  const status = document.getElementById('status');
  const handoff = location.hash.slice(1);
  // Remove the bearer credential from the visible URL/history immediately.
  history.replaceState(null, '', location.pathname);
  window.opener = null;
  if (!/^[a-f0-9]{64}$/.test(handoff)) {
    status.textContent = 'Return to the website and open chat from the widget again.';
    return;
  }

  const deadline = Date.now() + 25000;
  try {
    while (Date.now() < deadline) {
      const response = await fetch('/api/auth/popout/claim', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ handoff }),
        signal: AbortSignal.timeout(Math.max(1, deadline - Date.now())),
      });
      if (response.status === 202) {
        await new Promise(resolve => setTimeout(resolve, 300));
        continue;
      }
      if (!response.ok) throw new Error(`Sign-in failed (${response.status}).`);
      location.replace('/');
      return;
    }
    throw new Error('Opening chat timed out.');
  } catch (cause) {
    status.textContent = `Could not open chat: ${cause instanceof Error ? cause.message : String(cause)} Close this tab and try again from the widget.`;
  }
})();
