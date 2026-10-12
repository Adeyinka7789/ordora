// Reports page — dependency-free SVG trend chart.
//
// Data comes from window.__ORDORA_REPORTS__ (JSON embedded in the page).
// Previously this lazy-loaded Chart.js from a CDN, but the app's
// Content-Security-Policy only allows scripts from 'self' (plus the Tailwind
// CDN), so the browser blocked the download and the canvas stayed blank.
// This renderer draws grouped bars + a profit line with plain SVG, so the
// page always shows empty axes (never a blank box), even with no data or
// no network.

(function () {
  var PALETTE = {
    invoiced: '#0077BE',
    settled: '#006e4b',
    profit: '#d97706',
    grid: '#E1EAF2',
    tick: '#44566A'
  };

  function money(code, major) {
    try {
      return code + ' ' + Number(major).toLocaleString(undefined, {
        minimumFractionDigits: 2,
        maximumFractionDigits: 2
      });
    } catch (e) {
      return code + ' ' + major;
    }
  }

  function compact(v) {
    var a = Math.abs(v);
    if (a >= 1000000) return trimNum(v / 1000000) + 'M';
    if (a >= 1000) return trimNum(v / 1000) + 'k';
    return '' + Math.round(v);
  }

  function trimNum(n) {
    var s = (Math.round(n * 10) / 10).toString();
    return s;
  }

  // Round the axis ceiling up to a 1/2/5-step so ticks look sane.
  function niceCeil(v) {
    if (!(v > 0)) return 1;
    var exp = Math.floor(Math.log10(v));
    var base = Math.pow(10, exp);
    var frac = v / base;
    var nice = frac <= 1 ? 1 : frac <= 2 ? 2 : frac <= 5 ? 5 : 10;
    return nice * base;
  }

  function shortLabel(iso) {
    var d = new Date(iso);
    if (isNaN(d.getTime())) return '';
    try {
      return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
    } catch (e) {
      return d.toDateString();
    }
  }

  function render() {
    var host = document.getElementById('trend-chart');
    if (!host) return;
    var data = window.__ORDORA_REPORTS__ || {};
    var code = data.currency || '';
    var trend = Array.isArray(data.trend) ? data.trend : [];
    var profitRaw = Array.isArray(data.profit) ? data.profit : [];

    var n = trend.length;
    var labels = [];
    var invoiced = [];
    var settled = [];
    var i, p;
    for (i = 0; i < n; i++) {
      p = trend[i] || {};
      labels.push(shortLabel(p.bucket_start));
      invoiced.push((Number(p.invoiced_minor) || 0) / 100);
      settled.push((Number(p.settled_minor) || 0) / 100);
    }

    // Align profit by bucket_start when possible, fall back to index.
    var profit = [];
    if (profitRaw.length) {
      var byStart = {};
      for (i = 0; i < profitRaw.length; i++) {
        var q = profitRaw[i] || {};
        if (q.bucket_start) byStart[String(q.bucket_start)] = (Number(q.profit_minor) || 0) / 100;
      }
      var useMap = Object.keys(byStart).length === profitRaw.length && profitRaw.length === n;
      for (i = 0; i < n; i++) {
        if (useMap && trend[i] && trend[i].bucket_start && byStart[String(trend[i].bucket_start)] !== undefined) {
          profit.push(byStart[String(trend[i].bucket_start)]);
        } else if (profitRaw[i]) {
          profit.push((Number(profitRaw[i].profit_minor) || 0) / 100);
        } else {
          profit.push(0);
        }
      }
    }

    var hasProfit = profit.length === n && n > 0;
    var maxV = 0;
    for (i = 0; i < n; i++) {
      if (invoiced[i] > maxV) maxV = invoiced[i];
      if (settled[i] > maxV) maxV = settled[i];
      if (hasProfit && profit[i] > maxV) maxV = profit[i];
    }
    var allZero = maxV <= 0;
    var ceil = niceCeil(maxV);

    // Empty state: still draw axes below; show the overlay pill on top.
    var emptyEl = document.getElementById('trend-empty');
    if (emptyEl) {
      var showEmpty = n === 0 || allZero;
      emptyEl.classList.toggle('hidden', !showEmpty);
      emptyEl.classList.toggle('flex', showEmpty);
    }

    var W = Math.max(host.clientWidth || 0, 280);
    var H = Math.max(host.clientHeight || 0, 220);
    var M = { l: 64, r: 12, t: 14, b: 30 };
    var pw = W - M.l - M.r;
    var ph = H - M.t - M.b;

    var NS = 'http://www.w3.org/2000/svg';
    var svg = document.createElementNS(NS, 'svg');
    svg.setAttribute('width', '100%');
    svg.setAttribute('height', '100%');
    svg.setAttribute('viewBox', '0 0 ' + W + ' ' + H);
    svg.setAttribute('preserveAspectRatio', 'none');

    function el(name, attrs, parent) {
      var node = document.createElementNS(NS, name);
      for (var k in attrs) node.setAttribute(k, attrs[k]);
      parent.appendChild(node);
      return node;
    }

    function y(v) { return M.t + ph - (v / ceil) * ph; }

    // Gridlines + y ticks (always drawn, even when empty).
    var TICKS = 4;
    for (i = 0; i <= TICKS; i++) {
      var tv = (ceil * i) / TICKS;
      var yy = y(tv);
      el('line', { x1: M.l, y1: yy, x2: W - M.r, y2: yy, stroke: PALETTE.grid, 'stroke-width': 1 }, svg);
      var t = el('text', {
        x: M.l - 8, y: yy + 4, 'text-anchor': 'end',
        fill: PALETTE.tick, 'font-size': 11, 'font-family': 'Inter, sans-serif'
      }, svg);
      t.textContent = code ? code + ' ' + compact(tv) : compact(tv);
    }

    // X axis baseline.
    el('line', { x1: M.l, y1: M.t + ph, x2: W - M.r, y2: M.t + ph, stroke: PALETTE.grid, 'stroke-width': 1 }, svg);

    if (n > 0) {
      var slot = pw / n;
      var bw = Math.min(14, Math.max(4, (slot * 0.52) / 2));
      var gap = 2;
      // Thin out x labels when crowded.
      var step = 1;
      while (Math.ceil(n / step) > 8) step++;

      for (i = 0; i < n; i++) {
        var cx = M.l + slot * i + slot / 2;
        var x1 = cx - bw - gap / 2;
        var x2 = cx + gap / 2;
        var h1 = (invoiced[i] / ceil) * ph;
        var h2 = (settled[i] / ceil) * ph;

        var r1 = el('rect', {
          x: x1, y: M.t + ph - h1, width: bw, height: Math.max(h1, 0),
          rx: 2, fill: PALETTE.invoiced
        }, svg);
        var tt1 = document.createElementNS(NS, 'title');
        tt1.textContent = labels[i] + ' — Invoiced: ' + money(code, invoiced[i]);
        r1.appendChild(tt1);

        var r2 = el('rect', {
          x: x2, y: M.t + ph - h2, width: bw, height: Math.max(h2, 0),
          rx: 2, fill: PALETTE.settled
        }, svg);
        var tt2 = document.createElementNS(NS, 'title');
        tt2.textContent = labels[i] + ' — Settled: ' + money(code, settled[i]);
        r2.appendChild(tt2);

        if (i % step === 0) {
          var xt = el('text', {
            x: cx, y: H - 8, 'text-anchor': 'middle',
            fill: PALETTE.tick, 'font-size': 11, 'font-family': 'Inter, sans-serif'
          }, svg);
          xt.textContent = labels[i];
        }
      }

      // Profit line (only when the backend supplied profit buckets).
      if (hasProfit) {
        var pts = [];
        for (i = 0; i < n; i++) {
          var px = M.l + slot * i + slot / 2;
          var py = y(Math.max(profit[i], 0));
          pts.push(px.toFixed(1) + ',' + py.toFixed(1));
        }
        el('polyline', {
          points: pts.join(' '), fill: 'none',
          stroke: PALETTE.profit, 'stroke-width': 2, 'stroke-linejoin': 'round', 'stroke-linecap': 'round'
        }, svg);
        for (i = 0; i < n; i++) {
          var dx = M.l + slot * i + slot / 2;
          var dy = y(Math.max(profit[i], 0));
          var dot = el('circle', { cx: dx, cy: dy, r: 3, fill: PALETTE.profit }, svg);
          var ttd = document.createElementNS(NS, 'title');
          ttd.textContent = labels[i] + ' — Profit: ' + money(code, profit[i]);
          dot.appendChild(ttd);
        }
      }
    } else {
      var nt = el('text', {
        x: M.l + pw / 2, y: M.t + ph / 2,
        'text-anchor': 'middle', fill: PALETTE.tick,
        'font-size': 12, 'font-family': 'Inter, sans-serif'
      }, svg);
      nt.textContent = 'No buckets in this range';
    }

    host.innerHTML = '';
    host.appendChild(svg);
    // Keep the readable label accurate for assistive tech.
    host.setAttribute('aria-label', 'Revenue and cash flow chart' + (n === 0 || allZero ? ': no data in this range' : ''));
  }

  var timer = null;
  window.addEventListener('resize', function () {
    if (timer) clearTimeout(timer);
    timer = setTimeout(render, 150);
  });

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', render);
  } else {
    render();
  }
})();
