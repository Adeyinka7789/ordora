// Guided order creation (orders/new.html). Progressive enhancement over the
// plain form: steps, validation gates, review, sticky summary, mobile bar.
//
// The server owns validation and totals; this script only organizes the
// interaction. Panels render visible server-side; without JS the form works
// as one continuous page (step chrome hides itself via body.js-wizard).

(function () {
  'use strict';

  // Single-execution guard (see intake.js).
  if (window.__ordoraOrderSteps) return;
  window.__ordoraOrderSteps = true;

  var form = document.getElementById('order-form');
  if (!form) return;
  var panels = Array.prototype.slice.call(form.querySelectorAll('[data-step-panel]'));
  if (!panels.length) return;
  var navBtns = Array.prototype.slice.call(document.querySelectorAll('[data-step-goto]'));
  var announcer = document.getElementById('step-announcer');
  var stepError = document.getElementById('step-error');
  // Panel numbers in DOM order. Non-tailoring orgs render 1,2,4 (no
  // measurements step), so navigation follows this list, never raw +1/-1.
  var stepNums = panels.map(function (p) {
    return parseInt(p.getAttribute('data-step-panel'), 10);
  });
  var maxStep = stepNums[stepNums.length - 1];
  var current = stepNums[0];
  var visitedMax = 0; // index into stepNums
  function stepIndex(n) { return stepNums.indexOf(n); }
  function stepPosition(n) { return stepIndex(n) + 1; }

  // Panels render visible server-side (no-JS fallback = the full long
  // form). With JS, non-current panels hide via .wizard-hidden, which only
  // takes effect under body.js-wizard.
  function setPanelVisible(panel, visible) {
    if (visible) panel.classList.remove('wizard-hidden');
    else panel.classList.add('wizard-hidden');
  }

  function panelFor(n) {
    for (var i = 0; i < panels.length; i++) {
      if (panels[i].getAttribute('data-step-panel') === String(n)) return panels[i];
    }
    return null;
  }

  function announce(msg) {
    if (announcer) announcer.textContent = msg;
  }

  function showError(msg) {
    if (!stepError) return;
    if (!msg) {
      stepError.classList.add('hidden');
      stepError.textContent = '';
      return;
    }
    stepError.classList.remove('hidden');
    stepError.textContent = msg;
  }

  function syncNav() {
    navBtns.forEach(function (btn) {
      var n = parseInt(btn.getAttribute('data-step-goto'), 10);
      var dot = btn;
      var isCurrent = n === current;
      var isDone = stepIndex(n) !== -1 && stepIndex(n) < stepIndex(current);
      dot.classList.toggle('is-current', isCurrent);
      dot.classList.toggle('is-done', isDone);
      if (isCurrent) btn.setAttribute('aria-current', 'step');
      else btn.removeAttribute('aria-current');
      btn.disabled = stepIndex(n) === -1 || stepIndex(n) > visitedMax + 1;
    });
    var mobileNext = document.getElementById('mobile-next');
    if (mobileNext) mobileNext.textContent = current === maxStep ? 'Create Order' : 'Continue';
  }

  function focusHeading(panel) {
    var h = panel.querySelector('h2');
    if (h) {
      if (!h.hasAttribute('tabindex')) h.setAttribute('tabindex', '-1');
      try { h.focus({ preventScroll: true }); } catch (e) { try { h.focus(); } catch (e2) {} }
    }
    try { window.scrollTo({ top: 0, behavior: 'smooth' }); } catch (e) { window.scrollTo(0, 0); }
  }

  function rowValues(row) {
    function val(sel) {
      var el = row.querySelector(sel);
      return el ? (el.value || '').trim() : '';
    }
    return {
      desc: val('input[name$="[description]"]'),
      qty: val('input[name$="[quantity]"]'),
      price: val('input[name$="[unit_price]"]'),
    };
  }

  function validRows() {
    var rows = form.querySelectorAll('.item-row');
    var good = 0;
    for (var i = 0; i < rows.length; i++) {
      var v = rowValues(rows[i]);
      if (!v.desc && !v.qty && !v.price) continue; // blank row
      var qty = parseFloat(String(v.qty).replace(/,/g, ''));
      if (!v.desc || !(qty > 0)) return { ok: false, count: 0 };
      good++;
    }
    return { ok: good > 0, count: good };
  }

  function validateStep(n) {
    showError('');
    if (n === 1) {
      var cust = form.querySelector('#order-customer');
      var title = form.querySelector('input[name="title"]');
      if (!cust || !cust.value) {
        showError('Choose a customer to continue.');
        if (cust) cust.focus();
        return false;
      }
      if (!title || !title.value.trim()) {
        showError('Give the order a title to continue.');
        if (title) title.focus();
        return false;
      }
      return true;
    }
    if (n === 2) {
      var r = validRows();
      if (!r.ok) {
        showError('Add at least one item with a description and a quantity above zero.');
        var first = form.querySelector('.item-row input[name$="[description]"]');
        if (first) first.focus();
        return false;
      }
      return true;
    }
    return true;
  }

  function esc(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;')
      .replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function money(n) {
    var v = parseFloat(String(n).replace(/,/g, ''));
    if (isNaN(v)) v = 0;
    return v.toFixed(2);
  }

  function renderReview() {
    var host = document.getElementById('review-body');
    if (!host) return;
    function f(name) {
      var el = form.querySelector('[name="' + name + '"]');
      return el ? (el.value || '').trim() : '';
    }
    var custSel = form.querySelector('#order-customer');
    var custName = custSel && custSel.selectedIndex >= 0
      ? custSel.options[custSel.selectedIndex].text : '';
    var html = '<dl class="grid grid-cols-[140px_1fr] gap-x-space-md gap-y-space-sm font-body-md text-body-md">';
    html += '<dt class="text-on-surface-variant">Customer</dt><dd class="text-on-surface font-semibold">' + esc(custName) + '</dd>';
    html += '<dt class="text-on-surface-variant">Title</dt><dd class="text-on-surface font-semibold">' + esc(f('title')) + '</dd>';
    if (f('expected_completion')) html += '<dt class="text-on-surface-variant">Due</dt><dd class="text-on-surface">' + esc(f('expected_completion')) + '</dd>';
    if (f('description')) html += '<dt class="text-on-surface-variant">Notes</dt><dd class="text-on-surface whitespace-pre-wrap">' + esc(f('description')) + '</dd>';
    html += '</dl>';
    html += '<table class="w-full text-left"><thead><tr class="bg-surface-container-low font-label-sm text-label-sm text-outline uppercase tracking-wider"><th class="py-2 px-space-sm font-semibold rounded-l" scope="col">Item</th><th class="py-2 px-space-sm font-semibold text-center" scope="col">Qty</th><th class="py-2 px-space-sm font-semibold text-right rounded-r" scope="col">Subtotal</th></tr></thead><tbody>';
    var rows = form.querySelectorAll('.item-row');
    for (var i = 0; i < rows.length; i++) {
      var v = rowValues(rows[i]);
      if (!v.desc && !v.qty && !v.price) continue;
      var qty = parseFloat(String(v.qty).replace(/,/g, '')) || 0;
      var price = parseFloat(String(v.price).replace(/,/g, '')) || 0;
      var quote = v.price.trim() !== '' && price === 0 && rows[i].querySelector('input[name$="[product_id]"]') &&
        rows[i].querySelector('input[name$="[product_id]"]').value;
      html += '<tr class="border-b border-outline-variant/20"><td class="py-2 px-space-sm">' + esc(v.desc) +
        (quote ? ' <span class="font-label-sm text-label-sm font-semibold text-amber-800 bg-amber-100 px-2 py-0.5 rounded-full">Quote required</span>' : '') +
        '</td><td class="py-2 px-space-sm text-center tabular-nums">' + esc(v.qty) +
        '</td><td class="py-2 px-space-sm text-right tabular-nums">' + money(qty * price) + '</td></tr>';
    }
    html += '</tbody></table>';
    function tot(id) {
      var el = document.getElementById(id);
      return el ? el.textContent : '0.00';
    }
    html += '<dl class="flex flex-col gap-1 text-right tabular-nums font-body-md text-body-md">';
    html += '<div class="flex justify-between"><dt class="text-on-surface-variant">Subtotal</dt><dd>' + esc(tot('subtotal')) + '</dd></div>';
    html += '<div class="flex justify-between"><dt class="text-on-surface-variant">Discount / Tax</dt><dd>' + esc(tot('discount-total')) + ' / ' + esc(tot('tax-total')) + '</dd></div>';
    html += '<div class="flex justify-between font-bold text-headline-md"><dt>Total</dt><dd>' + esc(tot('grand-total')) + '</dd></div></dl>';
    host.innerHTML = html;
  }

  function nextStep() {
    var i = stepIndex(current);
    return stepNums[Math.min(i + 1, stepNums.length - 1)];
  }

  function prevStep() {
    var i = stepIndex(current);
    return stepNums[Math.max(i - 1, 0)];
  }

  function gotoStep(n, quiet) {
    if (stepIndex(n) === -1) n = stepNums[0];
    if (stepIndex(n) > stepIndex(current)) {
      // Validate every panel on the way (skips gaps: unknown steps pass).
      for (var s = stepIndex(current); s < stepIndex(n); s++) {
        if (!validateStep(stepNums[s])) {
          gotoStep(stepNums[s]);
          return;
        }
      }
    }
    current = n;
    if (stepIndex(n) > visitedMax) visitedMax = stepIndex(n);
    panels.forEach(function (p) {
      setPanelVisible(p, p.getAttribute('data-step-panel') === String(n));
    });
    if (n === maxStep) renderReview();
    syncNav();
    if (quiet) {
      refreshSummary();
      return;
    }
    var p = panelFor(n);
    if (p) {
      focusHeading(p);
      announce('Step ' + stepPosition(n) + ' of ' + stepNums.length);
    }
    refreshSummary();
  }

  // ---- Sticky summary + mobile bar mirror ----
  function refreshSummary() {
    var custSel = form.querySelector('#order-customer');
    var custName = custSel && custSel.selectedIndex > 0 ? custSel.options[custSel.selectedIndex].text : '—';
    var r = validRows();
    var totalEl = document.getElementById('grand-total');
    var total = totalEl ? totalEl.textContent : '0.00';
    function set(id, text) {
      var el = document.getElementById(id);
      if (el) el.textContent = text;
    }
    set('summary-customer', custName);
    set('summary-count', String(r.count));
    set('summary-total', total);
    set('mobile-total', total);
    var hasQuote = false;
    form.querySelectorAll('.item-row').forEach(function (row) {
      var badge = row.querySelector('.quote-badge');
      if (badge && !badge.hidden) hasQuote = true;
    });
    var note = document.getElementById('summary-quote-note');
    if (note) note.classList.toggle('hidden', !hasQuote);
  }

  // ---- Empty state + quote badges + row meta ----
  function refreshRows() {
    var rows = form.querySelectorAll('.item-row');
    var anyValue = false;
    rows.forEach(function (row) {
      var v = rowValues(row);
      if (v.desc || v.qty || v.price) anyValue = true;
      // Quote badge: linked product priced at zero.
      var pidEl = row.querySelector('input[name$="[product_id]"]');
      var price = parseFloat(String(v.price).replace(/,/g, ''));
      var badge = row.querySelector('.quote-badge');
      if (badge) badge.hidden = !(pidEl && pidEl.value && v.price.trim() !== '' && price === 0);
      // Material + thumbnail echo from hidden snapshot fields.
      var mat = row.querySelector('input[name$="[material]"]');
      var matEl = row.querySelector('.row-material');
      if (mat && matEl) matEl.textContent = mat.value || '';
      var imgRef = row.querySelector('input[name$="[image_ref]"]');
      var img = row.querySelector('img.row-thumb');
      if (imgRef && img) {
        if (imgRef.value) {
          img.src = '/attachments/' + imgRef.value + '?view=1';
          img.hidden = false;
        } else {
          img.hidden = true;
        }
      }
    });
    var empty = document.getElementById('items-empty');
    if (empty) empty.classList.toggle('hidden', rows.length > 0 && anyValue);
    refreshSummary();
  }

  // Stepper buttons (delegated: covers server + JS-added rows).
  document.addEventListener('click', function (e) {
    var btn = e.target && e.target.closest ? e.target.closest('.qty-dec,.qty-inc') : null;
    if (!btn || !form.contains(btn)) return;
    var row = btn.closest('.item-row');
    var input = row ? row.querySelector('.qty-input') : null;
    if (!input) return;
    var v = parseFloat(String(input.value).replace(/,/g, ''));
    if (isNaN(v)) v = 0;
    if (btn.classList.contains('qty-inc')) v = v + 1;
    else v = Math.max(0, Math.round((v - 1) * 1000) / 1000);
    input.value = (Math.round(v * 1000) / 1000).toString();
    input.dispatchEvent(new Event('input', { bubbles: true }));
  });

  form.addEventListener('input', function () { refreshRows(); });
  form.addEventListener('change', function () { refreshRows(); refreshSummary(); });
  document.addEventListener('click', function (e) {
    if (e.target && e.target.closest) {
      var t = e.target.closest('[data-step-next],[data-step-back],[data-step-goto],#mobile-next,.remove-item,#add-item,.picker-item');
      if (t) window.setTimeout(function () { refreshRows(); }, 0);
    }
  });

  document.addEventListener('click', function (e) {
    var next = e.target && e.target.closest ? e.target.closest('[data-step-next]') : null;
    if (next && form.contains(next)) { gotoStep(nextStep()); return; }
    var back = e.target && e.target.closest ? e.target.closest('[data-step-back]') : null;
    if (back && form.contains(back)) { gotoStep(prevStep()); return; }
    var go = e.target && e.target.closest ? e.target.closest('[data-step-goto]') : null;
    if (go) {
      var n = parseInt(go.getAttribute('data-step-goto'), 10);
      if (!isNaN(n)) gotoStep(n);
      return;
    }
    if (e.target && e.target.closest && e.target.closest('#mobile-next')) {
      if (current === maxStep) {
        try { form.requestSubmit(); } catch (err) { form.submit(); }
      } else {
        gotoStep(nextStep());
      }
    }
  });

  // Mirror totals into summary/mobile bar whenever orders.js recomputes.
  var grand = document.getElementById('grand-total');
  if (grand && window.MutationObserver) {
    new MutationObserver(function () { refreshSummary(); }).observe(grand, { childList: true, characterData: true, subtree: true });
  }

  // JS is running: enable wizard chrome (mobile bar) and show the first
  // step without stealing focus on load.
  try { document.body.classList.add('js-wizard'); } catch (e) {}
  var mobilebar = document.getElementById('order-mobilebar');
  if (mobilebar) mobilebar.hidden = false;
  syncNav();
  gotoStep(stepNums[0], true);
  refreshRows();
})();
