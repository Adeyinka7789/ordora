// Ordora central HTMX skeleton.
//
// Included once in layouts/app.html. Listens globally to HTMX events and
// injects a shimmer skeleton into the swap target's innerHTML while a request
// is in flight. One place to change the loading UX for every frontend.
//
// HTMX rigidity contract (do not break these):
//  1. Never replace or rename the target element. Only innerHTML is touched,
//     so hx-swap="outerHTML" always finds the original id (e.g. #orders-table).
//  2. Never inject a duplicate id. Skeleton markup uses classes only.
//  3. Never depend on hx-indicator placement. This works with zero per-page
//     hx-indicator attributes, so no swap target can lose its indicator.
//  4. Always clean up. On success HTMX replaces the target anyway; on error
//     the original innerHTML is restored so the page never sticks on skeleton.
//  5. Skip polling/heartbeat targets (e.g. #notif-badge) to avoid flicker.

(function () {
  'use strict';

  // Swap target id -> skeleton <template> id. Covers every hx-target in the
  // app layout. Unknown targets fall back to aria-busy + dim only (no DOM
  // injection), which is always swap-safe.
  var TARGET_TO_SKELETON = {
    // outerHTML table swaps (list pages)
    'orders-table': 'ordora-skeleton-table',
    'customers-table': 'ordora-skeleton-table',
    'products-table': 'ordora-skeleton-table',
    'ledger-table': 'ordora-skeleton-table',
    // outerHTML card swaps (order detail)
    'order-timeline': 'ordora-skeleton-card',
    'payments-card': 'ordora-skeleton-card',
    'costs-card': 'ordora-skeleton-card',
    'attachments': 'ordora-skeleton-card',
    // innerHTML swaps (live results)
    'picker-results': 'ordora-skeleton-list',
    'search-results': 'ordora-skeleton-list',
  };

  // Targets that must never show a skeleton (polling, badges, heartbeats).
  var SKIP_TARGETS = {
    'notif-badge': true,
  };

  // Delay before showing the skeleton; fast responses (< delay) never flash.
  var SHOW_DELAY_MS = 120;

  // Per-target in-flight state. WeakMap so entries vanish with the element.
  var pendingTimers = new WeakMap();
  var originalHTML = new WeakMap();

  function targetId(target) {
    return target && target.id ? target.id : '';
  }

  function skeletonTemplateId(id) {
    return TARGET_TO_SKELETON[id] || '';
  }

  function clearTimer(target) {
    var t = pendingTimers.get(target);
    if (t) {
      clearTimeout(t);
      pendingTimers.delete(target);
    }
  }

  function startLoading(target) {
    if (!target || !target.id) return;
    var id = targetId(target);
    if (SKIP_TARGETS[id]) return;
    if (target.hasAttribute('data-skeleton-active')) return;

    target.setAttribute('data-skeleton-active', '1');
    target.setAttribute('aria-busy', 'true');
    target.classList.add('ordora-loading');

    var tplId = skeletonTemplateId(id);
    if (!tplId) return; // unknown target: dim only, inject nothing.

    // Stash original markup for error-restore. Stored in a WeakMap, never in
    // a data-* attribute (large HTML in attributes is slow and lossy).
    if (!originalHTML.has(target)) {
      originalHTML.set(target, target.innerHTML);
    }

    var timer = setTimeout(function () {
      // Target may have been swapped away already; re-check liveness.
      if (!document.contains(target)) return;
      var tpl = document.getElementById(tplId);
      if (!tpl || !tpl.content) return;
      // Clone-only: the <template> stays in the layout untouched.
      var frag = tpl.content.cloneNode(true);
      target.innerHTML = '';
      target.appendChild(frag);
    }, SHOW_DELAY_MS);
    pendingTimers.set(target, timer);
  }

  function finishLoading(target, restoreOnError) {
    if (!target) return;
    clearTimer(target);
    target.removeAttribute('data-skeleton-active');
    target.removeAttribute('aria-busy');
    target.classList.remove('ordora-loading');
    if (restoreOnError && originalHTML.has(target)) {
      // Only restore if the skeleton is still showing (i.e. the swap did not
      // already replace the target with fresh server HTML).
      if (target.querySelector('.ordora-skeleton')) {
        target.innerHTML = originalHTML.get(target);
      }
      originalHTML.delete(target);
    } else {
      originalHTML.delete(target);
    }
  }

  function targetFromEvent(e) {
    // htmx 1.9: e.detail.target is the resolved swap-target element.
    // e.target is the requesting element (form/input). Prefer detail.target.
    if (e && e.detail && e.detail.target) return e.detail.target;
    return null;
  }

  document.body.addEventListener('htmx:beforeRequest', function (e) {
    startLoading(targetFromEvent(e));
  });

  // Success: HTMX has swapped fresh HTML; just drop loading state.
  document.body.addEventListener('htmx:afterSwap', function (e) {
    finishLoading(targetFromEvent(e) || e.target, false);
  });

  // Failure modes: restore the stashed markup so users never stick on a
  // shimmer. Covers 4xx/5xx, network errors, and swap failures (e.g. a
  // response whose root id does not match the target — the rigidity case).
  document.body.addEventListener('htmx:responseError', function (e) {
    finishLoading(targetFromEvent(e), true);
  });
  document.body.addEventListener('htmx:sendError', function (e) {
    finishLoading(targetFromEvent(e), true);
  });
  document.body.addEventListener('htmx:swapError', function (e) {
    finishLoading(targetFromEvent(e), true);
  });

  // Expose mapping for tests/debugging (read-only snapshot).
  window.ordoraSkeleton = window.ordoraSkeleton || {};
  window.ordoraSkeleton.targets = Object.freeze(Object.assign({}, TARGET_TO_SKELETON));
})();

