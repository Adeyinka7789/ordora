// Onboarding wizard — 4-step tour with slide transitions, progress bar,
// dots, back/next. Finish/skip are plain form POSTs (server persists the
// seen-flag); this script only switches panels. No-dependency, degrades to
// step 1 visible if JS is disabled.

(function () {
  'use strict';

  var container = document.getElementById('ob-steps');
  if (!container) return;
  var steps = Array.prototype.slice.call(container.querySelectorAll('.ob-step'));
  var dots = Array.prototype.slice.call(document.querySelectorAll('.ob-dot'));
  var back = document.getElementById('ob-back');
  var next = document.getElementById('ob-next');
  var fill = document.getElementById('ob-progress-fill');
  var count = document.getElementById('ob-count-now');
  var skipForm = document.getElementById('ob-skip-form');
  if (!steps.length) return;

  var current = 0;

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
    });
    dots.forEach(function (d, i) {
      d.classList.toggle('is-active', i === current);
    });
    if (fill) fill.style.width = ((current + 1) / steps.length * 100) + '%';
    if (count) count.textContent = String(current + 1);
    if (back) back.disabled = current === 0;
    if (next) next.style.visibility = current === steps.length - 1 ? 'hidden' : 'visible';
    if (skipForm) skipForm.style.visibility = current === steps.length - 1 ? 'hidden' : 'visible';
  }

  if (back) back.addEventListener('click', function () { show(current - 1, 'back'); });
  if (next) next.addEventListener('click', function () { show(current + 1, 'next'); });
  dots.forEach(function (d) {
    d.addEventListener('click', function () {
      show(parseInt(d.getAttribute('data-goto'), 10) || 0);
    });
  });

  show(0, 'next');
})();
