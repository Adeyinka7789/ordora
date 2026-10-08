// Order line-item manager. Vanilla JS, no framework.
//
// The server controls the form's data shape; this script only manages
// dynamic rows and live-computes the preview totals. It does NOT validate
// or submit — the form does that.

(function () {
  const body = document.getElementById('items-body');
  const addBtn = document.getElementById('add-item');
  const countInput = document.getElementById('item-count');
  if (!body || !addBtn || !countInput) return;

  function rowCount() {
    return body.querySelectorAll('.item-row').length;
  }

  function renumber() {
    const rows = body.querySelectorAll('.item-row');
    rows.forEach((row, i) => {
      row.querySelectorAll('input').forEach((input) => {
        const name = input.getAttribute('name');
        if (!name) return;
        const newName = name.replace(/items\[\d+\]/, 'items[' + i + ']');
        input.setAttribute('name', newName);
      });
    });
    countInput.value = String(rows.length);
  }

  function attachRemove(btn) {
    btn.addEventListener('click', () => {
      const row = btn.closest('.item-row');
      if (!row) return;
      if (rowCount() === 1) {
        // Don't remove the last row; just clear it.
        row.querySelectorAll('input').forEach((i) => (i.value = ''));
      } else {
        row.remove();
      }
      renumber();
      updateTotals();
    });
  }

  function attachLiveUpdate(row) {
    row.querySelectorAll('input').forEach((input) => {
      input.addEventListener('input', () => {
        updateRowSubtotal(row);
        updateTotals();
      });
    });
  }

  function parseAmount(s) {
    if (!s) return 0;
    const n = parseFloat(String(s).replace(/,/g, ''));
    return isNaN(n) ? 0 : n;
  }

  function updateRowSubtotal(row) {
    const qty = parseAmount(row.querySelector('.qty-input')?.value);
    const price = parseAmount(row.querySelector('.price-input')?.value);
    const cell = row.querySelector('.subtotal-cell');
    if (cell) cell.textContent = (qty * price).toFixed(2);
  }

  function updateTotals() {
    let subtotal = 0;
    body.querySelectorAll('.item-row').forEach((row) => {
      const qty = parseAmount(row.querySelector('.qty-input')?.value);
      const price = parseAmount(row.querySelector('.price-input')?.value);
      subtotal += qty * price;
    });

    const discount = parseAmount(document.querySelector('input[name="discount"]')?.value);
    const tax = parseAmount(document.querySelector('input[name="tax"]')?.value);
    const total = Math.max(0, subtotal - discount + tax);

    setText('subtotal', subtotal.toFixed(2));
    setText('discount-total', discount.toFixed(2));
    setText('tax-total', tax.toFixed(2));
    setText('grand-total', total.toFixed(2));
  }

  function setText(id, text) {
    const el = document.getElementById(id);
    if (el) el.textContent = text;
  }

  function addRow() {
    const tpl = body.querySelector('.item-row');
    let newRow;
    if (tpl) {
      newRow = tpl.cloneNode(true);
      newRow.querySelectorAll('input').forEach((i) => (i.value = ''));
      newRow.querySelector('.subtotal-cell').textContent = '—';
    } else {
      newRow = document.createElement('tr');
      newRow.className = 'item-row border-b border-outline-variant/20';
      newRow.innerHTML =
        '<td class="py-2 px-space-sm">' +
        '<input type="text" name="items[0][description]" placeholder="What is it?" required aria-label="Item description">' +
        '<input type="hidden" name="items[0][product_id]" value="">' +
        '<input type="hidden" name="items[0][material]" value="">' +
        '<input type="hidden" name="items[0][image_ref]" value="">' +
        '<div class="row-meta flex items-center gap-2 mt-1">' +
        '<img class="row-thumb w-10 h-10 rounded object-cover" alt="" hidden>' +
        '<span class="row-material font-body-sm text-body-sm text-on-surface-variant"></span>' +
        '<span class="quote-badge font-label-sm text-label-sm font-semibold text-amber-800 bg-amber-100 px-2 py-0.5 rounded-full" hidden>Quote required</span>' +
        '</div></td>' +
        '<td class="py-2 px-space-sm"><div class="flex items-center justify-center gap-1">' +
        '<button type="button" class="qty-dec w-8 h-8 rounded bg-surface-container hover:bg-surface-container-high text-on-surface font-bold" aria-label="Decrease quantity">−</button>' +
        '<input type="text" name="items[0][quantity]" class="qty-input w-14 h-8 px-2 rounded bg-surface-container-low font-code-md text-center" placeholder="1" required inputmode="decimal" aria-label="Quantity">' +
        '<button type="button" class="qty-inc w-8 h-8 rounded bg-surface-container hover:bg-surface-container-high text-on-surface font-bold" aria-label="Increase quantity">+</button>' +
        '</div></td>' +
        '<td class="py-2 px-space-sm"><input type="text" name="items[0][unit_price]" class="price-input w-full h-8 px-2 rounded bg-surface-container-low font-code-md text-right" placeholder="0.00" required inputmode="decimal" aria-label="Unit price"></td>' +
        '<td class="py-2 px-space-sm text-right tabular-nums subtotal-cell">—</td>' +
        '<td class="py-2 px-space-sm text-center"><button type="button" class="remove-item p-1 rounded hover:bg-error-container" aria-label="Remove item">×</button></td>';
    }
    body.appendChild(newRow);
    renumber();
    attachLiveUpdate(newRow);
    attachRemove(newRow.querySelector('.remove-item'));
    updateTotals();
  }

  // Wire existing rows.
  body.querySelectorAll('.item-row').forEach((row) => {
    attachLiveUpdate(row);
    attachRemove(row.querySelector('.remove-item'));
    updateRowSubtotal(row);
  });

  addBtn.addEventListener('click', addRow);

  // Live update on discount/tax changes.
  ['discount', 'tax'].forEach((name) => {
    const el = document.querySelector('input[name="' + name + '"]');
    if (el) el.addEventListener('input', updateTotals);
  });

  updateTotals();

    // ---- Product picker ----
  const pickBtn = document.getElementById('pick-product');
  const picker = document.getElementById('product-picker');
  const pickerClose = document.getElementById('picker-close');
  const pickerResults = document.getElementById('picker-results');

  if (pickBtn && picker && pickerClose) {
    pickBtn.addEventListener('click', () => {
      picker.hidden = !picker.hidden;
      if (!picker.hidden) {
        // Trigger initial load.
        const search = document.getElementById('picker-search');
        if (search) search.dispatchEvent(new Event('input'));
      }
    });
    pickerClose.addEventListener('click', () => { picker.hidden = true; });
  }

  // Delegate clicks on picker items.
  document.addEventListener('click', (e) => {
    const item = e.target.closest('.picker-item');
    if (!item) return;
    addRowFromProduct({
      id: item.dataset.id || '',
      name: item.dataset.name || '',
      price: item.dataset.price || '0.00',
      desc: item.dataset.desc || '',
      material: item.dataset.material || '',
      image: item.dataset.image || '',
    });
    picker.hidden = true;
  });

  function addRowFromProduct(prod) {
    // Add a new row, then fill its fields.
    addRow();
    const rows = body.querySelectorAll('.item-row');
    const lastRow = rows[rows.length - 1];
    if (!lastRow) return;

    const descInput = lastRow.querySelector('input[name$="[description]"]');
    const priceInput = lastRow.querySelector('.price-input');
    const qtyInput = lastRow.querySelector('.qty-input');
    const pidInput = lastRow.querySelector('input[name$="[product_id]"]');
    const matInput = lastRow.querySelector('input[name$="[material]"]');
    const imgInput = lastRow.querySelector('input[name$="[image_ref]"]');

    if (descInput) descInput.value = prod.name;
    if (priceInput) priceInput.value = prod.price;
    if (qtyInput && !qtyInput.value) qtyInput.value = '1';
    // Catalog snapshot link: frozen onto the line at order time.
    if (pidInput) pidInput.value = prod.id;
    if (matInput) matInput.value = prod.material;
    if (imgInput) imgInput.value = prod.image;
    // Visible snapshot echo (thumbnail + material + quote badge).
    const matEl = lastRow.querySelector('.row-material');
    if (matEl) matEl.textContent = prod.material || '';
    const thumb = lastRow.querySelector('img.row-thumb');
    if (thumb) {
      if (prod.image) {
        thumb.src = '/attachments/' + prod.image + '?view=1';
        thumb.hidden = false;
      } else {
        thumb.hidden = true;
      }
    }
    const badge = lastRow.querySelector('.quote-badge');
    if (badge) badge.hidden = !(prod.id && parseFloat(prod.price) === 0);

    updateRowSubtotal(lastRow);
    updateTotals();
  }
})();
