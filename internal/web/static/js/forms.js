// Ordora central form interactions (see layouts/app.html).
//
// One place for: submit-button spinners, the destructive-action confirm
// modal, copy-to-clipboard buttons, form drafts (localStorage), the
// session-expired dialog, flash-URL cleanup, and error focus.
//
// Conventions (set in templates, behavior lives here):
//   form[data-confirm]            ask before submit (data-confirm-title/label)
//   button[data-confirm-btn]      same, for buttons inside forms
//   button[data-copy-path]        copy absolute URL (e.g. "/order/abc")
//   button[data-copy-value]       copy literal text
//   form[data-draft="name"]       autosave + restore field values
//
// Everything degrades without JS: forms submit, copies just don't happen,
// drafts don't save. Honors prefers-reduced-motion where relevant.

(function () {
  'use strict';

  function $(sel, root) { return (root || document).querySelector(sel); }
  function $all(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }

  // ------------------------------------------------------------------
  // 1. Submit-button spinners (.is-loading in css/app.css).
  // ------------------------------------------------------------------
  function spinButton(btn) {
    if (!btn || btn.classList.contains('is-loading')) return;
    if (btn.disabled) return;
    btn.classList.add('is-loading');
    btn.setAttribute('aria-disabled', 'true');
    if (!btn.querySelector('.btn-label')) {
      var label = document.createElement('span');
      label.className = 'btn-label';
      while (btn.firstChild) label.appendChild(btn.firstChild);
      btn.appendChild(label);
    }
    btn.disabled = true;
  }

  function unspin(scope) {
    $all('.is-loading', scope).forEach(function (btn) {
      btn.classList.remove('is-loading');
      btn.removeAttribute('aria-disabled');
      btn.disabled = false;
    });
  }

  // Normal (non-HTMX) submits: spin the clicked submitter. bfcache/back
  // navigation restores disabled buttons, so always clear on pageshow.
  document.addEventListener('submit', function (e) {
    var form = e.target;
    if (!(form instanceof HTMLFormElement)) return;
    var btn = null;
    if (e.submitter instanceof HTMLElement) {
      btn = e.submitter;
    } else {
      btn = form.querySelector('[type="submit"]');
    }
    if (btn) spinButton(btn);
  }, true);
  window.addEventListener('pageshow', function () { unspin(document); });

  // HTMX swaps: spin the requestor, clear afterwards. The skeleton system
  // already covers swap targets; this covers the triggering control.
  document.addEventListener('htmx:beforeRequest', function (e) {
    var elt = e.detail && e.detail.elt;
    if (elt && (elt.tagName === 'BUTTON' || elt.tagName === 'A')) spinButton(elt);
  });
  document.addEventListener('htmx:afterRequest', function () { unspin(document); });
  document.addEventListener('htmx:responseError', function () { unspin(document); });

  // ------------------------------------------------------------------
  // 2. Central confirm modal (partials/confirm_modal.html).
  // ------------------------------------------------------------------
  var backdrop = null, modalTitle = null, modalBody = null, modalOk = null, modalCancel = null;
  var pendingForm = null, pendingBtn = null, lastTrigger = null;

  // Focus trap: while a modal backdrop is open, Tab cycles only through
  // the visible dialog's focusable controls. One document-level listener
  // covers both the confirm and session-expired dialogs.
  function trapTab(e) {
    if (e.key !== 'Tab') return;
    var open = null;
    ['ordora-confirm-backdrop', 'ordora-expired-backdrop'].forEach(function (id) {
      var b = document.getElementById(id);
      if (b && !b.hidden) open = b;
    });
    if (!open) return;
    var focusables = Array.prototype.slice.call(
      open.querySelectorAll('button, a[href], input, select, textarea, [tabindex]:not([tabindex="-1"])')
    ).filter(function (el) { return !el.disabled && el.offsetParent !== null; });
    if (!focusables.length) {
      e.preventDefault();
      return;
    }
    var first = focusables[0], last = focusables[focusables.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }
  document.addEventListener('keydown', trapTab, true);

  function ensureModal() {
    if (backdrop) return true;
    backdrop = $('#ordora-confirm-backdrop');
    if (!backdrop) return false;
    modalTitle = $('#ordora-confirm-title');
    modalBody = $('#ordora-confirm-body');
    modalOk = $('#ordora-confirm-ok');
    modalCancel = $('#ordora-confirm-cancel');
    modalOk.addEventListener('click', onConfirmOk);
    modalCancel.addEventListener('click', closeConfirm);
    backdrop.addEventListener('click', function (e) { if (e.target === backdrop) closeConfirm(); });
    document.addEventListener('keydown', function (e) {
      if (e.key === 'Escape' && backdrop && !backdrop.hidden) closeConfirm();
    });
    return true;
  }

  function openConfirm(trigger, form, text) {
    if (!ensureModal()) return false;
    lastTrigger = trigger;
    pendingForm = form;
    pendingBtn = (trigger instanceof HTMLButtonElement) ? trigger : null;
    if (modalTitle && trigger && trigger.getAttribute('data-confirm-title')) {
      modalTitle.textContent = trigger.getAttribute('data-confirm-title');
    } else if (modalTitle) {
      modalTitle.textContent = 'Are you sure?';
    }
    if (modalBody) modalBody.textContent = text;
    if (modalOk) {
      var label = (trigger && trigger.getAttribute('data-confirm-label')) || 'Confirm';
      var span = modalOk.querySelector('.btn-label');
      if (span) span.textContent = label; else modalOk.textContent = label;
      modalOk.classList.remove('is-loading');
      modalOk.disabled = false;
    }
    backdrop.hidden = false;
    if (modalOk) modalOk.focus();
    return true;
  }

  function closeConfirm() {
    if (backdrop) backdrop.hidden = true;
    pendingForm = null;
    pendingBtn = null;
    if (lastTrigger && lastTrigger.focus) {
      try { lastTrigger.focus(); } catch (e) {}
    }
    lastTrigger = null;
  }

  function onConfirmOk() {
    if (modalOk) spinButton(modalOk);
    var form = pendingForm;
    var btn = pendingBtn;
    closeConfirm();
    if (!form) return;
    form.dataset.confirmed = '1';
    if (btn) spinButton(btn);
    // requestSubmit re-fires the submit event: our interceptor sees the
    // confirmed flag and stands down, so HTMX forms still submit via
    // HTMX and plain forms submit natively. Native .submit() would
    // bypass HTMX and render a bare fragment page.
    try {
      if (btn && form.contains(btn) && btn.type === 'submit') form.requestSubmit(btn);
      else form.requestSubmit();
    } catch (e) {
      form.submit();
    }
  }

  // Intercept before the spinner listener runs (capture phase, registered
  // after it — both capture, so order by registration: spinner first would
  // spin then modal opens... instead check here and unspin).
  document.addEventListener('submit', function (e) {
    var form = e.target;
    if (!(form instanceof HTMLFormElement)) return;
    if (form.dataset.confirmed === '1') {
      delete form.dataset.confirmed;
      return;
    }
    var text = form.getAttribute('data-confirm');
    if (!text) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    unspin(document);
    openConfirm(document.activeElement, form, text);
  }, true);

  document.addEventListener('click', function (e) {
    var btn = e.target && e.target.closest ? e.target.closest('[data-confirm-btn]') : null;
    if (!btn) return;
    var form = btn.closest('form');
    if (!form) return;
    e.preventDefault();
    openConfirm(btn, form, btn.getAttribute('data-confirm-btn'));
  }, true);

  // ------------------------------------------------------------------
  // 3. Copy buttons ([data-copy-path] / [data-copy-value]).
  // ------------------------------------------------------------------
  function flashCopied(btn) {
    var original = btn.getAttribute('data-copy-original');
    if (original === null) {
      original = btn.innerHTML;
      btn.setAttribute('data-copy-original', original);
    }
    btn.innerHTML = '<span aria-hidden="true" class="material-symbols-outlined text-[16px]">check</span><span>Copied</span>';
    window.setTimeout(function () { btn.innerHTML = original; }, 1500);
  }

  document.addEventListener('click', function (e) {
    var btn = e.target && e.target.closest ? e.target.closest('[data-copy-path],[data-copy-value]') : null;
    if (!btn || btn.disabled) return;
    var text = btn.getAttribute('data-copy-value') || '';
    if (!text && btn.getAttribute('data-copy-path')) {
      text = window.location.origin + btn.getAttribute('data-copy-path');
    }
    if (!text || !navigator.clipboard) return;
    e.preventDefault();
    navigator.clipboard.writeText(text).then(function () {
      flashCopied(btn);
    }).catch(function () {
      window.prompt('Copy this link:', text);
    });
  });

  // ------------------------------------------------------------------
  // 3b. Print buttons ([data-print]) and auto-submitting controls
  // ([data-autosubmit] submit their form on change).
  // ------------------------------------------------------------------
  document.addEventListener('click', function (e) {
    var btn = e.target && e.target.closest ? e.target.closest('[data-print]') : null;
    if (!btn) return;
    e.preventDefault();
    window.print();
  });

  document.addEventListener('change', function (e) {
    var el = e.target && e.target.closest ? e.target.closest('[data-autosubmit]') : null;
    if (!el) return;
    var form = el.closest('form');
    if (form) form.requestSubmit();
  });

  // ------------------------------------------------------------------
  // 4. Drafts: autosave [data-draft] forms, restore empty fields.
  // ------------------------------------------------------------------
  function draftKey(form) {
    return 'ordora-draft:' + (form.getAttribute('data-draft') || window.location.pathname);
  }

  function serializable(el) {
    if (!el.name || el.disabled) return false;
    if (/^(password|file)$/.test(el.type)) return false;
    if (el.name === '_csrf') return false;
    // Dynamic grids (order items, measurements) are server-rendered on
    // error; restoring them risks count mismatch, so skip by prefix.
    if (/^(items\[|measure\[)/.test(el.name)) return false;
    return /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName);
  }

  function readDraft(form) {
    try {
      var raw = localStorage.getItem(draftKey(form));
      return raw ? JSON.parse(raw) : {};
    } catch (e) { return {}; }
  }

  function saveDraft(form) {
    var data = {};
    $all('input,textarea,select', form).forEach(function (el) {
      if (!serializable(el)) return;
      if ((el.type === 'checkbox' || el.type === 'radio')) {
        data[el.name] = el.checked ? '1' : '';
      } else {
        data[el.name] = el.value;
      }
    });
    try { localStorage.setItem(draftKey(form), JSON.stringify(data)); } catch (e) {}
  }

  function restoreDraft(form) {
    var data = readDraft(form);
    var names = Object.keys(data);
    if (!names.length) return;
    $all('input,textarea,select', form).forEach(function (el) {
      if (!serializable(el)) return;
      if (!(el.name in data)) return;
      var v = data[el.name];
      if ((el.type === 'checkbox' || el.type === 'radio')) {
        if (!el.checked && v === '1' && !el.hasAttribute('data-draft-skip')) el.checked = true;
        return;
      }
      // Only fill blanks: server-rendered values (error re-renders) win.
      if ((el.value === '' || el.value === null) && v !== '') el.value = v;
    });
  }

  var saveTimers = new WeakMap();
  document.addEventListener('input', function (e) {
    var form = e.target && e.target.closest ? e.target.closest('form[data-draft]') : null;
    if (!form) return;
    var t = saveTimers.get(form);
    if (t) window.clearTimeout(t);
    saveTimers.set(form, window.setTimeout(function () { saveDraft(form); }, 400));
  }, true);
  // A submitted form either succeeded (draft stale) or re-rendered with
  // server values (draft harmless) — either way, drop it.
  document.addEventListener('submit', function (e) {
    var form = e.target;
    if (form instanceof HTMLFormElement && form.hasAttribute('data-draft')) {
      try { localStorage.removeItem(draftKey(form)); } catch (err) {}
    }
  }, true);

  // ------------------------------------------------------------------
  // 5. Session expiry: HTMX landing on /login means the session died.
  // ------------------------------------------------------------------
  function showExpired() {
    var b = $('#ordora-expired-backdrop');
    if (!b) return;
    b.hidden = false;
    var link = b.querySelector('a');
    if (link) link.focus();
  }

  document.addEventListener('htmx:afterRequest', function (e) {
    try {
      var xhr = e.detail && e.detail.xhr;
      var target = e.detail && e.detail.target;
      if (!xhr || !xhr.responseURL) return;
      if (target && target.id === 'notif-badge') return; // background poll
      if (xhr.responseURL.indexOf('/login') !== -1) showExpired();
    } catch (err) {}
  });

  // ------------------------------------------------------------------
  // 6. One-time flashes: strip notice/error from the URL after render.
  // ------------------------------------------------------------------
  try {
    var url = new URL(window.location.href);
    if (url.searchParams.has('notice') || url.searchParams.has('error')) {
      url.searchParams.delete('notice');
      url.searchParams.delete('error');
      window.history.replaceState(null, '', url.toString());
    }
  } catch (e) {}

  // ------------------------------------------------------------------
  // 7. Focus the first invalid field (server-rendered aria-invalid).
  // ------------------------------------------------------------------
  document.addEventListener('DOMContentLoaded', function () {
    $all('form[data-draft]').forEach(restoreDraft);
    var bad = document.querySelector('[aria-invalid="true"]');
    if (bad && bad.focus) {
      try {
        bad.focus({ preventScroll: false });
        bad.scrollIntoView({ block: 'center', behavior: 'smooth' });
      } catch (e) {
        try { bad.focus(); } catch (e2) {}
      }
    }
  });
})();
