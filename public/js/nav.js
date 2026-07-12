// =========================================================
// nav.js — shared nav bar + drawer, injected into .app-wrapper.
// One copy of the header instead of one per page. Must be included
// BEFORE the Alpine CDN script: defer scripts run in document order,
// so the markup exists by the time Alpine scans the DOM. The page's
// <body> keeps x-data="{ navOpen: false }".
// =========================================================
(function () {
  const page = location.pathname.split('/').pop() || 'index.html';
  const active = (href) => (href === page ? ' active' : '');

  const html = `
    <nav class="nav">
      <a href="index.html" class="nav-brand">
        <span class="brand-icon">⚽</span>
        <span class="brand-text">WC <span>Fantasy</span></span>
      </a>
      <div style="display:flex;align-items:center;gap:8px">
        <div class="spinner" x-show="$store.auth.authLoading" x-cloak></div>
        <div class="nav-avatar" x-cloak
             x-show="!$store.auth.authLoading && $store.auth.isLoggedIn"
             :class="'avatar-' + $store.auth.user?.colorIdx"
             x-text="$store.auth.user?.initials"></div>
        <button class="nav-hamburger" @click="navOpen = !navOpen" aria-label="Menu">
          <span x-text="navOpen ? '✕' : '☰'"></span>
        </button>
      </div>
    </nav>
    <div x-show="navOpen" x-cloak @click="navOpen = false"
         style="position:fixed;inset:0;z-index:150"></div>
    <div class="nav-drawer" x-show="navOpen" x-cloak>
      <a href="index.html" class="nav-drawer-link${active('index.html')}">Standings</a>
      <a href="draft.html" class="nav-drawer-link${active('draft.html')}" x-show="$store.auth.isLoggedIn">Draft</a>
      <a href="teams.html" class="nav-drawer-link${active('teams.html')}" x-show="$store.auth.isLoggedIn">Teams</a>
      <a href="my-team.html" class="nav-drawer-link${active('my-team.html')}" x-show="$store.auth.isLoggedIn">My Team</a>
      <a href="admin.html" class="nav-drawer-link${active('admin.html')}" x-show="$store.auth.isAdmin">Admin</a>
      <div class="nav-drawer-divider"></div>
      <a href="login.html" class="nav-drawer-link${active('login.html')}" x-show="!$store.auth.isLoggedIn && !$store.auth.authLoading">Sign In</a>
      <button class="nav-drawer-link nav-drawer-signout" x-show="$store.auth.isLoggedIn" @click="signOutUser()">Sign Out</button>
    </div>`;

  const wrapper = document.querySelector('.app-wrapper');
  if (wrapper) wrapper.insertAdjacentHTML('afterbegin', html);
})();
