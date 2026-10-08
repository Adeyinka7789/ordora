// Public intake catalog (portal/intake.html). Progressive enhancement over
// the plain form: search/category filter, add-to-request + steppers,
// selected states, product detail dialog, live request summary, and a
// review step. Without JS the qty inputs submit directly.

(function () {
  'use strict';

  // Single-execution guard: the script must never bind twice even if a
  // template includes it more than once (duplicate listeners would double
  // steppers and risk dialog.showModal InvalidStateError).
  if (window.__ordoraIntake) return;
  window.__ordoraIntake = true;

  var grid = document.getElementById('catalog-grid');
  if (!grid) return;

  function cards() {
    return Array.prototype.slice.call(grid.querySelectorAll('.catalog-card'));
  }

  function qtyOf(card) {
    var input = card.querySelector('.catalog-qty');
    if (!input) return 0;
    var v = parseFloat(String(input.value).replace(/,/g, ''));
    return isNaN(v) || v < 0 ? 0 : v;
  }

  function setQty(card, v) {
    var input = card.querySelector('.catalog-qty');
    if (!input) return;
    if (isNaN(v) || v < 0) v = 0;
    input.value = v > 0 ? String(Math.round(v * 1000) / 1000) : '';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    syncCard(card);
    renderSummary();
  }

  function syncCard(card) {
    var selected = qtyOf(card) > 0;
    card.classList.toggle('is-selected', selected);
    var add = card.querySelector('.catalog-add');
    var stepper = card.querySelector('.catalog-stepper');
    if (add) add.classList.toggle('hidden', selected);
    if (stepper) {
      stepper.classList.toggle('hidden', !selected);
      stepper.classList.toggle('flex', selected);
    }
  }

  function syncAll() { cards().forEach(syncCard); }

  // ---- Search + category filter ----
  var search = document.getElementById('catalog-search');
  var cat = document.getElementById('catalog-category');
  var empty = document.getElementById('catalog-empty');
  function applyFilter() {
    var q = search ? (search.value || '').toLowerCase().trim() : '';
    var c = cat ? cat.value : '';
    var shown = 0;
    cards().forEach(function (el) {
      var okQ = !q || (el.getAttribute('data-search') || '').toLowerCase().indexOf(q) >= 0;
      var okC = !c || el.getAttribute('data-category') === c;
      var show = okQ && okC;
      el.style.display = show ? '' : 'none';
      if (show) shown++;
    });
    if (empty) empty.classList.toggle('hidden', shown > 0);
  }
  if (search) search.addEventListener('input', applyFilter);
  if (cat) cat.addEventListener('change', applyFilter);

  // ---- Add / steppers (delegated) ----
  document.addEventListener('click', function (e) {
    var add = e.target && e.target.closest ? e.target.closest('[data-card-add]') : null;
    if (add) {
      var card = add.closest('.catalog-card');
      if (!card) return;
      setQty(card, Math.max(1, qtyOf(card) || 1));
      var stepInput = card.querySelector('.catalog-qty');
      if (stepInput) stepInput.focus();
      return;
    }
    var inc = e.target && e.target.closest ? e.target.closest('[data-step-inc]') : null;
    var dec = e.target && e.target.closest ? e.target.closest('[data-step-dec]') : null;
    var btn = inc || dec;
    if (btn) {
      var c2 = btn.closest('.catalog-card');
      if (!c2) return;
      var v = qtyOf(c2) + (inc ? 1 : -1);
      setQty(c2, v);
    }
  });
  grid.addEventListener('input', function (e) {
    var card = e.target && e.target.closest ? e.target.closest('.catalog-card') : null;
    if (!card) return;
    if (e.target.classList && e.target.classList.contains('catalog-qty')) {
      syncCard(card);
      renderSummary();
    }
  });

  // ---- Detail dialog ----
  var dialog = document.getElementById('product-dialog');
  var dialogTitle = document.getElementById('product-dialog-title');
  var dialogGallery = document.getElementById('product-dialog-gallery');
  var dialogFacts = document.getElementById('product-dialog-facts');
  var dialogDesc = document.getElementById('product-dialog-desc');
  var dialogAdd = document.getElementById('product-dialog-add');
  var dialogClose = document.getElementById('product-dialog-close');
  var dialogCard = null;

  function esc(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;')
      .replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function openDetails(card) {
    if (!dialog || !card) return;
    dialogCard = card;
    var d = card.dataset;
    if (dialogTitle) dialogTitle.textContent = d.name || 'Product';
    if (dialogGallery) {
      var ids = (d.images || '').split(',').filter(function (x) { return x; });
      var html = '';
      ids.forEach(function (id) {
        html += '<img src="/public/product-images/' + encodeURIComponent(id) + '" alt="" loading="lazy" class="w-24 h-24 rounded-lg object-cover shrink-0 bg-surface-container-low">';
      });
      dialogGallery.innerHTML = html;
      dialogGallery.style.display = ids.length ? '' : 'none';
    }
    if (dialogFacts) {
      var rows = [];
      if (d.material) rows.push(['Material', d.material]);
      if (d.color) rows.push(['Color', d.color]);
      if (d.category) rows.push(['Category', d.category]);
      if (d.availability === 'low_stock') rows.push(['Availability', 'Low stock']);
      if (d.availability === 'made_to_order') rows.push(['Availability', 'Made to order']);
      if (d.proddays && d.proddays !== '0') rows.push(['Lead time', '~' + d.proddays + ' days']);
      if (d.specs) rows.push(['Specs', d.specs]);
      dialogFacts.innerHTML = rows.map(function (r) {
        return '<dt class="text-on-surface-variant">' + esc(r[0]) + '</dt><dd class="text-on-surface">' + esc(r[1]) + '</dd>';
      }).join('');
      dialogFacts.style.display = rows.length ? '' : 'none';
    }
    if (dialogDesc) {
      dialogDesc.textContent = d.desc || '';
      dialogDesc.style.display = d.desc ? '' : 'none';
    }
    if (typeof dialog.showModal === 'function') dialog.showModal();
    else dialog.setAttribute('open', '');
  }

  document.addEventListener('click', function (e) {
    var btn = e.target && e.target.closest ? e.target.closest('[data-card-details]') : null;
    if (btn) {
      var card = btn.closest('.catalog-card');
      openDetails(card);
    }
  });
  if (dialogClose) dialogClose.addEventListener('click', function () { dialog.close(); });
  if (dialogAdd) dialogAdd.addEventListener('click', function () {
    if (dialogCard) setQty(dialogCard, Math.max(1, qtyOf(dialogCard) || 1));
    dialog.close();
  });
  if (dialog) dialog.addEventListener('click', function (e) {
    if (e.target === dialog) dialog.close();
  });

  // ---- Live request summary ----
  var linesHost = document.getElementById('request-lines');
  var totalHost = document.getElementById('request-total');
  var emptyHost = document.getElementById('request-empty');

  function money(minor, currency) {
    var v = (minor / 100).toFixed(2);
    return (currency ? currency + ' ' : '') + v;
  }

  function selected() {
    var out = [];
    cards().forEach(function (card) {
      var q = qtyOf(card);
      if (q > 0) out.push({ card: card, qty: q });
    });
    return out;
  }

  function renderSummary() {
    if (!linesHost) return;
    var items = selected();
    var html = '';
    var total = 0;
    var currency = '';
    items.forEach(function (it) {
      var d = it.card.dataset;
      var price = parseFloat(d.price) || 0;
      currency = d.currency || currency;
      var line = it.qty * price;
      if (!d.quote) total += line;
      html += '<li class="flex justify-between gap-2 font-body-md text-body-md">' +
        '<span class="text-on-surface">' + esc(d.name) + ' × ' + it.qty +
        (d.quote ? ' <span class="font-label-sm text-label-sm font-semibold text-amber-800">· quote</span>' : '') + '</span>' +
        '<span class="tabular-nums text-on-surface-variant">' + (d.quote ? 'quote' : money(line, '')) + '</span></li>';
    });
    linesHost.innerHTML = html;
    if (emptyHost) emptyHost.style.display = items.length ? 'none' : '';
    if (totalHost) totalHost.textContent = items.length ? money(total, currency) : '—';
  }

  // ---- Review step ----
  var reviewBtn = document.getElementById('review-open');
  var reviewPanel = document.getElementById('intake-review');
  var reviewBody = document.getElementById('review-body');
  var reviewBack = document.getElementById('review-back');
  var actions = document.getElementById('intake-actions');
  var form = grid.closest('form');

  function field(name) {
    if (!form) return '';
    var el = form.querySelector('[name="' + name + '"]');
    return el ? (el.value || '').trim() : '';
  }

  if (reviewBtn) reviewBtn.addEventListener('click', function () {
    if (!reviewPanel || !form) return;
    var items = selected();
    var html = '<dl class="grid grid-cols-[140px_1fr] gap-x-space-sm gap-y-1 font-body-md text-body-md">';
    html += '<dt class="text-on-surface-variant">Name</dt><dd class="text-on-surface font-semibold">' + esc(field('customer_name')) + '</dd>';
    var contact = field('customer_email') + (field('customer_email') && field('customer_phone') ? ' · ' : '') + field('customer_phone');
    html += '<dt class="text-on-surface-variant">Contact</dt><dd class="text-on-surface">' + esc(contact || '—') + '</dd>';
    html += '</dl>';
    if (items.length) {
      html += '<ul class="flex flex-col gap-1">';
      items.forEach(function (it) {
        var d = it.card.dataset;
        html += '<li class="flex justify-between gap-2 font-body-md text-body-md"><span class="text-on-surface">' +
          esc(d.name) + ' × ' + it.qty + '</span><span class="tabular-nums text-on-surface-variant">' +
          (d.quote ? 'Request quote' : money(it.qty * (parseFloat(d.price) || 0), d.currency)) + '</span></li>';
      });
      html += '</ul>';
    }
    if (field('description')) {
      html += '<div><div class="font-label-md text-label-md text-on-surface font-semibold">Anything else</div>' +
        '<p class="font-body-md text-body-md text-on-surface whitespace-pre-wrap">' + esc(field('description')) + '</p></div>';
    }
    reviewBody.innerHTML = html;
    ['order-card', 'request-summary'].forEach(function (id) {
      var el = document.getElementById(id);
      if (el) el.hidden = true;
    });
    reviewPanel.hidden = false;
    if (actions) actions.hidden = true;
    var h = document.getElementById('review-h');
    if (h) { h.focus(); }
    window.scrollTo(0, 0);
  });

  if (reviewBack) reviewBack.addEventListener('click', function () {
    if (!reviewPanel || !form) return;
    reviewPanel.hidden = true;
    ['order-card', 'request-summary'].forEach(function (id) {
      var el = document.getElementById(id);
      if (el) el.hidden = false;
    });
    if (actions) actions.hidden = false;
  });

  syncAll();
  renderSummary();
})();
