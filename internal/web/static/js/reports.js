// Reports page — lazy-loads Chart.js and initializes the trend chart.
//
// Data comes from window.__ORDORA_REPORTS__ (JSON embedded in the page).
// Chart.js is loaded from a CDN only when this page is visited.

(function () {
  var data = window.__ORDORA_REPORTS__;
  if (!data) return;

  // Lazy-load Chart.js.
  function loadChartJS(callback) {
    if (window.Chart) return callback();
    var s = document.createElement('script');
    s.src = 'https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js';
    s.onload = callback;
    document.head.appendChild(s);
  }

  function renderTrend() {
    var el = document.getElementById('trend-chart');
    if (!el) return;

    var labels = data.trend.map(function (p) {
      var d = new Date(p.bucket_start);
      return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
    });
    var invoiced = data.trend.map(function (p) { return p.invoiced_minor / 100; });
    var settled = data.trend.map(function (p) { return p.settled_minor / 100; });

    new window.Chart(el, {
      type: 'bar',
      data: {
        labels: labels,
              datasets: [
        {
          label: 'Invoiced',
          data: invoiced,
          backgroundColor: '#005f6b',
          borderRadius: 4,
          barPercentage: 0.7,
          categoryPercentage: 0.6
        },
        {
          label: 'Settled',
          data: settled,
          backgroundColor: '#006e4b',
          borderRadius: 4,
          barPercentage: 0.7,
          categoryPercentage: 0.6
        },
        {
          label: 'Profit',
          type: 'line',
          data: (data.profit || []).map(function (p) { return p.profit_minor / 100; }),
          borderColor: '#d97706',
          backgroundColor: '#d97706',
          borderWidth: 2,
          pointRadius: 3,
          pointBackgroundColor: '#d97706',
          tension: 0.3,
          fill: false,
          yAxisID: 'y'
        }
      ]
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        interaction: { mode: 'index', intersect: false },
        plugins: {
          legend: {
            position: 'bottom',
            labels: {
              color: '#455556',
              font: { family: 'Inter', size: 12 },
              boxWidth: 10,
              usePointStyle: true
            }
          },
          tooltip: {
            backgroundColor: '#102e33',
            titleColor: '#e6f0f0',
            bodyColor: '#e6f0f0',
            padding: 10,
            callbacks: {
              label: function (ctx) {
                var v = ctx.parsed.y;
                return ctx.dataset.label + ': ₦' + v.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 });
              }
            }
          }
        },
        scales: {
          x: {
            grid: { display: false },
            ticks: { color: '#455556', font: { family: 'Inter', size: 11 } }
          },
          y: {
            beginAtZero: true,
            grid: { color: '#e0ebeb' },
            ticks: {
              color: '#455556',
              font: { family: 'Inter', size: 11 },
              callback: function (v) { return '₦' + v.toLocaleString(); }
            }
          }
        }
      }
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', function () { loadChartJS(renderTrend); });
  } else {
    loadChartJS(renderTrend);
  }
})();
