(function () {
  'use strict';
  // Deployment-specific URL only; the complete Docker app needs no setting.
  try {
    if (!window.LUMO_API_BASE) window.LUMO_API_BASE = localStorage.getItem('lumo-api-base-v1') || '';
  } catch (_) {}
})();
