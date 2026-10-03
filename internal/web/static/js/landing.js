// Ordora landing page — interactivity.
//
// Three small behaviors:
//   1. Scroll reveal via IntersectionObserver
//   2. Number count-up for stats
//   3. Smooth scroll for in-page anchor links
//
// Everything degrades gracefully if JS is disabled.

(function () {
  // ---- 1. Scroll reveal ----
  var revealEls = document.querySelectorAll('[data-reveal]');
  if ('IntersectionObserver' in window && revealEls.length) {
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) {
          e.target.classList.add('revealed');
          io.unobserve(e.target);
        }
      });
    }, { threshold: 0.12, rootMargin: '0px 0px -60px 0px' });
    revealEls.forEach(function (el) { io.observe(el); });
  } else {
    // No IO support — just show everything.
    revealEls.forEach(function (el) { el.classList.add('revealed'); });
  }

  // ---- 2. Number count-up ----
  var countEls = document.querySelectorAll('[data-count-to]');
  function animateCount(el) {
    var target = parseInt(el.getAttribute('data-count-to'), 10);
    if (isNaN(target)) return;
    var suffix = el.getAttribute('data-count-suffix') || '';
    var prefix = el.getAttribute('data-count-prefix') || '';
    var duration = 1200;
    var start = performance.now();
    function step(now) {
      var t = Math.min(1, (now - start) / duration);
      // easeOutExpo
      var eased = t === 1 ? 1 : 1 - Math.pow(2, -10 * t);
      var current = Math.round(eased * target);
      el.textContent = prefix + current.toLocaleString() + suffix;
      if (t < 1) requestAnimationFrame(step);
    }
    requestAnimationFrame(step);
  }
  if ('IntersectionObserver' in window && countEls.length) {
    var io2 = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) {
          animateCount(e.target);
          io2.unobserve(e.target);
        }
      });
    }, { threshold: 0.5 });
    countEls.forEach(function (el) { io2.observe(el); });
  }

  // ---- 3. Smooth scroll for same-page anchors ----
  document.querySelectorAll('a[href^="#"]').forEach(function (a) {
    a.addEventListener('click', function (ev) {
      var id = a.getAttribute('href');
      if (id === '#' || id.length < 2) return;
      var target = document.querySelector(id);
      if (!target) return;
      ev.preventDefault();
      target.scrollIntoView({ behavior: 'smooth', block: 'start' });
      history.replaceState(null, '', id);
    });
  });
})();
