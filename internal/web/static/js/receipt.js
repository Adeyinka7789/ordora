// Receipt downloads: PNG share image + copy-link.
//
// The PNG is a fixed-size (1080x1350) OPay-style share card drawn as pure SVG
// from the #receipt-card data-* attributes — no HTML serialization, so it
// always fits a phone screen and is always centered, no matter how long the
// full receipt is. The full line-item breakdown stays in Print/PDF and on
// the receipt link. No external assets are embedded, keeping the canvas
// untainted.

(function () {
  'use strict';

  function filename() {
    var num = 'receipt';
    var card = document.getElementById('receipt-card');
    if (card) {
      var el = card.querySelector('.rcpt-number');
      if (el && el.textContent.trim()) num = el.textContent.trim();
    }
    return num.replace(/[^A-Za-z0-9-_]+/g, '-') + '.png';
  }

  function esc(s) {
    return String(s == null ? '' : s)
      .replace(/&/g, '&amp;').replace(/</g, '&lt;')
      .replace(/>/g, '&gt;').replace(/"/g, '&quot;');
  }

  function trunc(s, n) {
    s = String(s == null ? '' : s);
    return s.length > n ? s.slice(0, n - 1) + '…' : s;
  }

  // Fixed share-card geometry (SVG units). Rasterized at SHARE_SCALE, so the
  // file is crisp on any screen while the composition never changes.
  var SHARE_W = 1080;
  var SHARE_H = 1350;
  var SHARE_SCALE = 2;
  var SHARE_FONT = 'Segoe UI, Inter, Helvetica, Arial, sans-serif';

  function shareData() {
    var node = document.getElementById('receipt-card');
    var d = node ? node.dataset : {};
    return {
      org: d.org || '', amount: d.amount || '', total: d.total || '',
      order: d.order || '', customer: d.customer || '', date: d.date || '',
      status: d.status || '', methods: d.methods || '', link: d.link || ''
    };
  }

  // Builds the fixed-size share card. All coordinates are absolute, so the
  // output is identical on every device and always fits a phone screen.
  function shareSVG(r) {
    var paid = r.status === 'PAID';
    var badgeFill = paid ? '#6ffbbe' : '#E1EAF2';
    var badgeText = paid ? '#002113' : '#44566A';

    var rows = [
      ['Customer', r.customer],
      ['Order', r.order],
      ['Date', r.date],
      ['Paid via', r.methods],
      ['Status', paid ? 'PAID' : (r.status || '—')]
    ];
    var rowY = 610, rowStep = 84, rowSVG = '';
    for (var i = 0; i < rows.length; i++) {
      var y = rowY + i * rowStep;
      rowSVG +=
        '<text x="130" y="' + y + '" font-family="' + SHARE_FONT + '" font-size="28" fill="#707A88">' + esc(trunc(rows[i][0], 20)) + '</text>' +
        '<text x="950" y="' + y + '" text-anchor="end" font-family="' + SHARE_FONT + '" font-size="30" font-weight="600" fill="#0B1C30">' + esc(trunc(rows[i][1] || '—', 34)) + '</text>';
    }

    return '' +
      '<svg xmlns="http://www.w3.org/2000/svg" width="' + SHARE_W + '" height="' + SHARE_H + '" viewBox="0 0 ' + SHARE_W + ' ' + SHARE_H + '">' +
      '<rect x="0" y="0" width="' + SHARE_W + '" height="' + SHARE_H + '" fill="#F7FAFD"/>' +
      '<rect x="54" y="54" width="972" height="1242" rx="40" fill="#ffffff" stroke="#E1EAF2" stroke-width="2"/>' +
      '<text x="540" y="160" text-anchor="middle" font-family="' + SHARE_FONT + '" font-size="36" font-weight="700" fill="#0B1C30">' + esc(trunc(r.org || 'Receipt', 30)) + '</text>' +
      '<text x="540" y="206" text-anchor="middle" font-family="' + SHARE_FONT + '" font-size="24" letter-spacing="4" fill="#707A88">RECEIPT</text>' +
      '<rect x="430" y="238" width="220" height="52" rx="26" fill="' + badgeFill + '"/>' +
      '<text x="540" y="273" text-anchor="middle" font-family="' + SHARE_FONT + '" font-size="26" font-weight="700" fill="' + badgeText + '">' + esc(trunc(paid ? 'PAID' : (r.status || '—'), 16)) + '</text>' +
      '<text x="540" y="420" text-anchor="middle" font-family="' + SHARE_FONT + '" font-size="92" font-weight="700" fill="#005338">' + esc(trunc(r.amount || r.total, 24)) + '</text>' +
      '<text x="540" y="468" text-anchor="middle" font-family="' + SHARE_FONT + '" font-size="28" fill="#44566A">of ' + esc(trunc(r.total, 24)) + '</text>' +
      '<line x1="130" y1="522" x2="950" y2="522" stroke="#C3D2DE" stroke-width="2" stroke-dasharray="10 8"/>' +
      rowSVG +
      '<line x1="130" y1="1070" x2="950" y2="1070" stroke="#C3D2DE" stroke-width="2" stroke-dasharray="10 8"/>' +
      '<text x="540" y="1130" text-anchor="middle" font-family="' + SHARE_FONT + '" font-size="28" fill="#44566A">Thank you for your patronage.</text>' +
      '<text x="540" y="1178" text-anchor="middle" font-family="' + SHARE_FONT + '" font-size="24" fill="#0077BE">' + esc(trunc(r.link, 64)) + '</text>' +
      '<text x="540" y="1222" text-anchor="middle" font-family="' + SHARE_FONT + '" font-size="24" fill="#707A88">Full breakdown available as PDF.</text>' +
      '</svg>';
  }

  async function downloadPNG() {
    var node = document.getElementById('receipt-card');
    if (!node) return;
    var btn = document.getElementById('receipt-png-btn');
    var original = btn ? btn.innerHTML : '';
    try {
      if (btn) btn.innerHTML = 'Working…';

      var svg = shareSVG(shareData());

      var img = new Image();
      var url = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(svg);
      await new Promise(function (resolve, reject) {
        img.onload = resolve;
        img.onerror = reject;
        img.src = url;
      });

      var canvas = document.createElement('canvas');
      canvas.width = SHARE_W * SHARE_SCALE;
      canvas.height = SHARE_H * SHARE_SCALE;
      var ctx = canvas.getContext('2d');
      ctx.fillStyle = '#F7FAFD';
      ctx.fillRect(0, 0, canvas.width, canvas.height);
      ctx.drawImage(img, 0, 0, canvas.width, canvas.height);

      var a = document.createElement('a');
      a.download = filename();
      a.href = canvas.toDataURL('image/png');
      document.body.appendChild(a);
      a.click();
      a.remove();
    } catch (e) {
      alert('Could not create the image. Use Print / PDF instead.');
    } finally {
      if (btn) btn.innerHTML = original;
    }
  }

  async function copyText(text, btn) {
    try {
      await navigator.clipboard.writeText(text);
    } catch (e) {
      var ta = document.createElement('textarea');
      ta.value = text;
      document.body.appendChild(ta);
      ta.select();
      try { document.execCommand('copy'); } catch (_e) {}
      ta.remove();
    }
    if (btn) {
      var original = btn.innerHTML;
      btn.innerHTML = 'Copied ✓';
      setTimeout(function () { btn.innerHTML = original; }, 1600);
    }
  }

  document.addEventListener('DOMContentLoaded', function () {
    var png = document.getElementById('receipt-png-btn');
    if (png) png.addEventListener('click', downloadPNG);

    ['receipt-copy-btn', 'receipt-client-copy-btn'].forEach(function (id) {
      var b = document.getElementById(id);
      if (b && b.dataset.copy) {
        b.addEventListener('click', function () { copyText(b.dataset.copy, b); });
      }
    });
  });
})();


