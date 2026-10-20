// Plain script so host pages need no framework or build tooling.
(() => {
  const script = document.currentScript;
  if (!script || document.querySelector('iframe[data-orgchat-widget]')) return;
  const config = window.orgchatConfig || {};
  const embed = new URL('embed.html', script.src);
  embed.searchParams.set('parentOrigin', location.origin);
  const frame = document.createElement('iframe');
  frame.dataset.orgchatWidget = '';
  frame.title = 'orgchat';
  frame.src = embed.href;
  Object.assign(frame.style, {
    visibility: 'hidden',
    pointerEvents: 'none',
    position: 'fixed',
    right: 'max(16px, env(safe-area-inset-right))',
    bottom: 'max(16px, env(safe-area-inset-bottom))',
    width: 'min(380px, calc(100vw - 32px))',
    height: '56px',
    border: '0',
    borderRadius: '12px',
    background: 'transparent',
    colorScheme: 'normal',
    zIndex: '2147483000',
  });

  function resize(expanded) {
    const mobile = window.matchMedia('(max-width: 480px)').matches;
    frame.style.width = mobile && !expanded
      ? '144px' : 'min(380px, calc(100vw - 32px))';
    frame.style.height = expanded
      ? (mobile ? 'min(480px, 70dvh)' : 'min(540px, calc(100dvh - 32px))')
      : (mobile ? '44px' : '56px');
  }

  let expanded = false;
  window.addEventListener('message', event => {
    if (event.source !== frame.contentWindow || event.origin !== embed.origin) return;
    if (event.data?.type === 'orgchat:visibility' && typeof event.data.visible === 'boolean') {
      frame.style.visibility = event.data.visible ? 'visible' : 'hidden';
      frame.style.pointerEvents = event.data.visible ? 'auto' : 'none';
      return;
    }
    if (event.data?.type === 'orgchat:auth-required') {
      const requestId = event.data.requestId;
      if (typeof requestId !== 'string' || requestId.length > 128) return;
      Promise.resolve().then(() => typeof config.getToken === 'function' ? config.getToken() : null)
        .then(token => {
          frame.contentWindow?.postMessage({
            type: 'orgchat:auth-token', requestId,
            token: typeof token === 'string' ? token : null,
          }, embed.origin);
        }, () => {
          frame.contentWindow?.postMessage({ type: 'orgchat:auth-token', requestId, token: null }, embed.origin);
        });
      return;
    }
    if (event.data?.type !== 'orgchat:resize' || typeof event.data.expanded !== 'boolean') return;
    const changed = expanded !== event.data.expanded;
    expanded = event.data.expanded;
    resize(expanded);
    // Don't steal focus when the launcher first loads on the host page.
    if (changed) frame.contentWindow.postMessage({ type: 'orgchat:focus' }, embed.origin);
  });
  window.addEventListener('resize', () => resize(expanded));
  resize(expanded);
  document.body.append(frame);
})();
