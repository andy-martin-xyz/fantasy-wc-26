// =========================================================
// auth.js — Firebase Auth integration (Google Sign-In).
// Depends on: config.js, firebase-app-compat.js, firebase-auth-compat.js, app.js
// =========================================================

(function () {
  'use strict';

  // Guard against misconfigured projects.
  if (!FIREBASE_CONFIG || FIREBASE_CONFIG.apiKey === 'REPLACE_ME') {
    console.warn('[auth] Firebase not configured. Edit js/config.js before using auth features.');
    // Mark auth as resolved (not loading) so the UI doesn't hang on a spinner.
    document.addEventListener('alpine:init', () => {
      Alpine.store('auth').authLoading = false;
    });
    return;
  }

  // Initialize Firebase app (idempotent).
  if (!firebase.apps.length) {
    firebase.initializeApp(FIREBASE_CONFIG);
  }

  const auth = firebase.auth();

  // --- Public API on window ------------------------------------

  /** Opens a Google sign-in popup. Returns the Firebase user credential. */
  window.signInWithGoogle = async function () {
    const provider = new firebase.auth.GoogleAuthProvider();
    return auth.signInWithPopup(provider);
  };

  /** Signs out the current user. */
  window.signOutUser = async function () {
    return auth.signOut();
  };

  /**
   * Returns the current user's Firebase ID token, or null if not signed in.
   * Tokens are refreshed automatically if close to expiry.
   */
  window.getIdToken = async function () {
    const user = auth.currentUser;
    if (!user) return null;
    return user.getIdToken(/* forceRefresh */ false);
  };

  // --- Auth state listener -------------------------------------

  auth.onAuthStateChanged(async (firebaseUser) => {
    if (firebaseUser) {
      await handleSignedIn(firebaseUser);
    } else {
      handleSignedOut();
    }
  });

  async function handleSignedIn(firebaseUser) {
    const token = await firebaseUser.getIdToken();

    // Build a default store entry immediately so the nav renders.
    const storeUser = {
      uid:         firebaseUser.uid,
      displayName: firebaseUser.displayName || firebaseUser.email,
      email:       firebaseUser.email,
      photoURL:    firebaseUser.photoURL || '',
      teamName:    '',
      initials:    getInitials(firebaseUser.displayName || firebaseUser.email),
      colorIdx:    uidToColorIdx(firebaseUser.uid),
    };

    Alpine.store('auth', {
      isLoggedIn:  true,
      isAdmin:     false,
      authLoading: false,
      user:        storeUser,
    });

    // Register / fetch user profile from the backend.
    // POST /api/user/register is idempotent — safe to call every sign-in.
    try {
      const defaultTeamName = (firebaseUser.displayName || 'Player') + "'s Team";
      const profile = await fetchJSON('POST', '/api/user/register', { teamName: defaultTeamName }, token);
      Alpine.store('auth').user.teamName = profile.teamName;
      Alpine.store('auth').isAdmin      = profile.isAdmin;
    } catch (e) {
      console.warn('[auth] register/profile fetch failed:', e);
    }

    // If this is the login page, redirect home after sign-in.
    if (currentPage() === 'login.html') {
      const dest = sessionStorage.getItem('authRedirect') || 'index.html';
      sessionStorage.removeItem('authRedirect');
      window.location.href = dest;
    }
  }

  function handleSignedOut() {
    Alpine.store('auth', {
      isLoggedIn:  false,
      isAdmin:     false,
      authLoading: false,
      user:        null,
    });

    // Protect pages that require login.
    const protected_ = ['draft.html', 'my-team.html', 'admin.html'];
    if (protected_.includes(currentPage())) {
      sessionStorage.setItem('authRedirect', window.location.href);
      window.location.href = 'login.html';
    }
  }

  // --- Helpers -------------------------------------------------

  function currentPage() {
    return window.location.pathname.split('/').pop() || 'index.html';
  }

  function getInitials(name) {
    if (!name) return '?';
    return name.trim().split(/\s+/).map(n => n[0]).join('').slice(0, 2).toUpperCase();
  }

  // Consistent color index for a given UID (0-7).
  function uidToColorIdx(uid) {
    let h = 0;
    for (let i = 0; i < uid.length; i++) {
      h = (uid.charCodeAt(i) + ((h << 5) - h)) | 0;
    }
    return Math.abs(h) % 8;
  }

  async function fetchJSON(method, path, body, token) {
    const url = (API_BASE || '') + path;
    const headers = { 'Content-Type': 'application/json' };
    if (token) headers['Authorization'] = 'Bearer ' + token;
    const res = await fetch(url, {
      method,
      headers,
      body: body ? JSON.stringify(body) : undefined,
    });
    if (!res.ok) throw new Error(await res.text());
    return res.json();
  }
})();
