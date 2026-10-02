// PWA registration + install prompt.
//
// Registers the service worker after page load (so it doesn't compete with
// first-paint bandwidth). Also captures the beforeinstallprompt event and
// shows a small "Install app" banner.

(function () {
  // Register the service worker.
  if ('serviceWorker' in navigator) {
    window.addEventListener('load', () => {
      navigator.serviceWorker
        .register('/static/sw.js', { scope: '/' })
        .then((reg) => {
          // Optional: check for updates on load.
          reg.update().catch(() => {});
        })
        .catch((err) => {
          console.warn('Service worker registration failed:', err);
        });
    });
  }

  // Capture the install prompt.
  let deferredPrompt = null;
  const banner = document.getElementById('pwa-install-banner');
  const installBtn = document.getElementById('pwa-install-btn');
  const dismissBtn = document.getElementById('pwa-install-dismiss');

  window.addEventListener('beforeinstallprompt', (e) => {
    e.preventDefault();
    deferredPrompt = e;
    if (banner) banner.classList.remove('hidden');
  });

  if (installBtn) {
    installBtn.addEventListener('click', async () => {
      if (!deferredPrompt) return;
      deferredPrompt.prompt();
      const { outcome } = await deferredPrompt.userChoice;
      deferredPrompt = null;
      if (banner) banner.classList.add('hidden');
      console.log('Install prompt outcome:', outcome);
    });
  }

  if (dismissBtn) {
    dismissBtn.addEventListener('click', () => {
      if (banner) banner.classList.add('hidden');
      // Remember the dismissal for this session.
      try { sessionStorage.setItem('pwa-install-dismissed', '1'); } catch {}
    });
  }

  // If already dismissed this session, hide the banner on load.
  try {
    if (sessionStorage.getItem('pwa-install-dismissed') === '1' && banner) {
      banner.classList.add('hidden');
    }
  } catch {}

  // If already installed, hide the banner.
  window.addEventListener('appinstalled', () => {
    if (banner) banner.classList.add('hidden');
    deferredPrompt = null;
  });
})();
