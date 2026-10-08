// Ordora shared chrome — utility bar, scroll-up button, section spotlight.
//
// Loaded by the landing + app layouts. Three behaviors:
//   1. Utility top bar hides on scroll-down, returns on scroll-up/top.
//      The nav/topbar underneath never moves away (pure CSS offsets).
//   2. Scroll-up button appears only after scrolling down.
//   3. Landing sections: only the current section shows color; sections
//      scrolled past go grey (no-op where [data-spotlight] is absent).
// Everything degrades gracefully without JS (bars visible, no floats gating
// content) and honors prefers-reduced-motion.

(function () {
  'use strict';

  var reduceMotion = false;
  try {
    reduceMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  } catch (e) {}

  var lastY = window.scrollY || 0;
  var ticking = false;
  var spots = Array.prototype.slice.call(document.querySelectorAll('[data-spotlight]'));

  function markSpotlight() {
    if (!spots.length) return;
    var middle = window.scrollY + window.innerHeight * 0.45;
    var current = spots[0];
    for (var i = 0; i < spots.length; i++) {
      if (spots[i].offsetTop <= middle) current = spots[i];
    }
    spots.forEach(function (s) {
      s.classList.toggle('is-current', s === current);
      s.classList.toggle('is-past', s !== current && s.offsetTop < current.offsetTop);
    });
  }

  function onScroll() {
    ticking = false;
    var y = window.scrollY || 0;

    document.body.classList.toggle('scrolled-down', y > 600);

    if (y > 80 && y > lastY + 4) {
      document.body.classList.add('util-hidden');
    } else if (y < lastY - 4 || y <= 80) {
      document.body.classList.remove('util-hidden');
    }
    lastY = y;

    markSpotlight();
  }

  window.addEventListener('scroll', function () {
    if (!ticking) {
      ticking = true;
      window.requestAnimationFrame(onScroll);
    }
  }, { passive: true });

  var top = document.getElementById('scroll-top-btn');
  if (top) {
    top.addEventListener('click', function () {
      window.scrollTo({ top: 0, behavior: reduceMotion ? 'auto' : 'smooth' });
    });
  }

  // ---- 4. Mobile bottom tab bar: menu opens the same drawer as the
  // topbar toggler, and the tab matching the URL gets the active state.
  var menuBtn = document.getElementById('mobile-menu-btn');
  if (menuBtn) {
    menuBtn.addEventListener('click', function (e) {
      e.stopPropagation();
      var toggler = document.getElementById('sidebar-toggler');
      if (toggler) {
        toggler.click(); // reuse the exact drawer behavior
      } else {
        document.body.classList.toggle('sidebar-open');
      }
    });
  }
  var tabs = Array.prototype.slice.call(document.querySelectorAll('[data-mnav]'));
  var snavs = Array.prototype.slice.call(document.querySelectorAll('[data-snav]'));
  if (tabs.length || snavs.length) {
    // Hub sections highlight their tab; every sidebar destination
    // highlights its own link. Anywhere else nothing is active.
    var path = window.location.pathname || '/';
    var section = '';
    if (path === '/' || path.indexOf('/dashboard') === 0) section = 'dashboard';
    else if (path.indexOf('/customers') === 0) section = 'customers';
    else if (path.indexOf('/products') === 0) section = 'products';
    else if (path.indexOf('/payments') === 0) section = 'payments';
    else if (path.indexOf('/groups') === 0 || path.indexOf('/g/') === 0) section = 'groups';
    else if (path.indexOf('/calendar') === 0) section = 'calendar';
    else if (path.indexOf('/storefront') === 0) section = 'storefront';
    else if (path.indexOf('/reports') === 0) section = 'reports';
    else if (path.indexOf('/settings') === 0) section = 'settings';
    else if (path.indexOf('/profile') === 0) section = 'profile';
    else if (path.indexOf('/support') === 0 || path.indexOf('/notifications') === 0) section = 'support';
    else if (path.indexOf('/orders') === 0 || path.indexOf('/search') === 0) section = 'orders';
    tabs.forEach(function (t) {
      // Mobile tabs only cover the three hubs.
      var hub = (section === 'dashboard' || section === 'orders' || section === 'customers') ? section : '';
      var active = t.getAttribute('data-mnav') === hub;
      t.classList.toggle('is-active', active);
      if (active) t.setAttribute('aria-current', 'page');
      else t.removeAttribute('aria-current');
    });
    snavs.forEach(function (t) {
      var active = t.getAttribute('data-snav') === section;
      t.classList.toggle('is-active', active);
      if (active) t.setAttribute('aria-current', 'page');
      else t.removeAttribute('aria-current');
    });
  }

  markSpotlight();
  onScroll();

  // ---- 5. Connection state: sync chip, offline banner, toasts ----
  var banner = document.getElementById('offline-banner');
  var retryBtn = document.getElementById('offline-retry');

  window.OrdoraToast = function (msg, isError) {
    var host = document.getElementById('ordora-toasts');
    if (!host) return;
    var el = document.createElement('div');
    el.className = 'ordora-toast' + (isError ? ' is-error' : '');
    el.setAttribute('role', 'status');
    el.textContent = msg;
    host.appendChild(el);
    window.setTimeout(function () {
      if (el.parentNode) el.parentNode.removeChild(el);
    }, 3500);
  };

  function stampUpdated() {
    var el = document.getElementById('sync-updated');
    if (!el) return;
    try {
      el.textContent = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    } catch (e) {
      el.textContent = 'just now';
    }
  }

  function setOnline(online) {
    document.body.classList.toggle('is-offline', !online);
    if (banner) banner.hidden = online;
    var label = document.querySelector('#sync-chip .sync-label');
    if (label) label.textContent = online ? 'Live Sync: Connected' : 'Live Sync: Offline';
    if (online) {
      stampUpdated();
      window.OrdoraToast('Back online — refreshing.');
      // Revalidate swap targets that may be stale? Keep it light: only
      // refresh explicitly live regions (notification badge).
      try {
        var badge = document.getElementById('notif-badge');
        if (badge && window.htmx) window.htmx.trigger(badge, 'refresh');
      } catch (e) {}
    } else {
      window.OrdoraToast('You are offline.', true);
    }
  }

  window.addEventListener('online', function () { setOnline(true); });
  window.addEventListener('offline', function () { setOnline(false); });
  if (retryBtn) retryBtn.addEventListener('click', function () { window.location.reload(); });
  if (!window.navigator.onLine) setOnline(false);
  else stampUpdated();
  document.addEventListener('htmx:afterRequest', function () { stampUpdated(); });
})();
