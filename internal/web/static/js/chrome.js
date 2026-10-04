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
  if (tabs.length) {
    // Only the three hub sections highlight; anywhere else no tab is active.
    var path = window.location.pathname || '/';
    var section = '';
    if (path === '/' || path.indexOf('/dashboard') === 0) section = 'dashboard';
    else if (path.indexOf('/orders') === 0) section = 'orders';
    else if (path.indexOf('/customers') === 0) section = 'customers';
    tabs.forEach(function (t) {
      var active = t.getAttribute('data-mnav') === section;
      t.classList.toggle('is-active', active);
      if (active) t.setAttribute('aria-current', 'page');
      else t.removeAttribute('aria-current');
    });
  }

  markSpotlight();
  onScroll();
})();
