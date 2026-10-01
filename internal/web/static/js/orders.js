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
      newRow.className = 'item-row';
      newRow.innerHTML =
        '<td><input type="text" name="items[0][description]" required></td>' +
        '<td><input type="text" name="items[0][quantity]" class="qty-input" required></td>' +
        '<td><input type="text" name="items[0][unit_price]" class="price-input" required></td>' +
        '<td class="subtotal-cell">—</td>' +
        '<td><button type="button" class="remove-item" aria-label="Remove item">×</button></td>';
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
})();
