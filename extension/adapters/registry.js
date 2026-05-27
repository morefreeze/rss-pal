// extension/adapters/registry.js
//
// Per-site adapter registry, populated by each adapter file at content_script
// load time. MV3 content_scripts don't support ES module import, so we use
// IIFE self-registration on a global namespace.
//
// Each adapter calls:
//   window.__rssPalAdapters.register({ site, name, sourceKind, domain,
//     urlPattern, pullable, passive, extract })
//
// content.js then asks findFor(location) for the matching adapter.

(function () {
  'use strict';

  if (window.__rssPalAdapters) return;  // idempotent

  const adapters = [];

  function register(adapter) {
    if (!adapter || typeof adapter.extract !== 'function') {
      console.warn('[rss-pal] adapter missing extract():', adapter);
      return;
    }
    adapters.push(adapter);
  }

  function findFor(loc) {
    for (const a of adapters) {
      if (a.domain && loc.hostname !== a.domain) continue;
      if (a.urlPattern && !a.urlPattern.test(loc.pathname)) continue;
      return a;
    }
    return null;
  }

  function listPullable() {
    return adapters.filter((a) => a.pullable);
  }

  window.__rssPalAdapters = { register, findFor, listPullable, _all: adapters };
})();
