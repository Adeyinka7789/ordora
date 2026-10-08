// Onboarding wizard — 4 task-first steps with real tab semantics.
//
// Panels render visible server-side; without JS the page is one continuous
// (fully usable) flow and the step nav stays hidden via body.js-ob.
// Finish/skip/customer-submit are plain form POSTs; this script only
// switches panels, moves focus, and announces changes.

(function () {
  'use strict';

  if (window.__ordoraOnboarding) return;
  window.__ordoraOnboarding = true;

  var container = document.getElementById('ob-steps');
  if (!container) return;
  var steps = Array.prototype.slice.call(container.querySelectorAll('.ob-step'));
  var dots = Array.prototype.slice.call(document.querySelectorAll('.ob-dot'));
  var back = document.getElementById('ob-back');
  var next = document.getElementById('ob-next');
  var fill = document.getElementById('ob-progress-fill');
  var count = document.getElementById('ob-count-now');
  var announcer = document.getElementById('ob-announcer');
  var skipForm = document.getElementById('ob-skip-form');
  if (!steps.length) return;

  // JS drives the wizard from here: reveal step chrome, collapse to one.
  try { document.body.classList.add('js-ob'); } catch (e) {}

  var current = 0;
  try {
    var start = parseInt(container.getAttribute('data-start'), 10);
    if (!isNaN(start) && start >= 0 && start < steps.length) current = start;
  } catch (e) {}

  function label(n) {
    var d = dots[n];
    return d ? (d.getAttribute('aria-label') || ('Step ' + (n + 1))) : ('Step ' + (n + 1));
  }

  function show(n, dir) {
    if (n < 0) n = 0;
    if (n > steps.length - 1) n = steps.length - 1;
    dir = dir || (n >= current ? 'next' : 'back');
    current = n;
    container.setAttribute('data-dir', dir);
    steps.forEach(function (s, i) {
      var active = i === current;
      s.classList.toggle('is-active', active);
      if (active) {
        s.removeAttribute('hidden');
        // Restart the entrance animation.
        s.style.animation = 'none';
        void s.offsetWidth; // eslint-disable-line no-unused-expressions
        s.style.animation = '';
      } else {
        s.setAttribute('hidden', '');
      }
      s.setAttribute('aria-hidden', active ? 'false' : 'true');
    });
    dots.forEach(function (d, i) {
      var active = i === current;
      d.classList.toggle('is-active', active);
      d.setAttribute('aria-selected', active ? 'true' : 'false');
      d.tabIndex = active ? 0 : -1;
    });
    if (fill) fill.style.width = ((current + 1) / steps.length * 100) + '%';
    if (count) count.textContent = String(current + 1);
    if (back) back.disabled = current === 0;
    var last = current === steps.length - 1;
    if (next) next.hidden = last;
    if (skipForm) skipForm.hidden = last;
    if (announcer) announcer.textContent = label(current);
    // Move focus to the step heading so keyboard and screen-reader users
    // land on the new content (skip on initial render).
    if (dir !== 'init') {
      var h = steps[current].querySelector('h1');
      if (h) {
        if (!h.hasAttribute('tabindex')) h.setAttribute('tabindex', '-1');
        try { h.focus({ preventScroll: true }); } catch (e) { try { h.focus(); } catch (e2) {} }
      }
    }
  }

  // Roving tabindex across tabs (arrow-key pattern).
  function focusTab(n) {
    if (n < 0) n = dots.length - 1;
    if (n > dots.length - 1) n = 0;
    var d = dots[n];
    if (d) d.focus();
  }

  if (back) back.addEventListener('click', function () { show(current - 1, 'back'); });
  if (next) next.addEventListener('click', function () { show(current + 1, 'next'); });
  // In-panel path buttons (step 1 choices) jump like tabs.
  Array.prototype.slice.call(document.querySelectorAll('[data-goto-path]')).forEach(function (b) {
    b.addEventListener('click', function () {
      var n = parseInt(b.getAttribute('data-goto-path'), 10);
      if (!isNaN(n)) show(n);
    });
  });
  dots.forEach(function (d, i) {
    d.addEventListener('click', function () { show(i); });
    d.addEventListener('keydown', function (e) {
      if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
        e.preventDefault();
        focusTab(i + 1);
      } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
        e.preventDefault();
        focusTab(i - 1);
      } else if (e.key === 'Home') {
        e.preventDefault();
        focusTab(0);
      } else if (e.key === 'End') {
        e.preventDefault();
        focusTab(dots.length - 1);
      }
    });
  });

  show(current, 'init');
})();
