// =========================================================
// api.js — API client for the Go backend.
// Depends on: config.js, auth.js (for getIdToken)
// All functions return parsed JSON or throw on error.
// =========================================================

(function () {
  'use strict';

  // --- Core fetch wrapper --------------------------------------

  async function apiCall(method, path, body) {
    const url = (API_BASE || '') + path;
    const headers = { 'Content-Type': 'application/json' };

    // Attach Firebase ID token when the user is signed in.
    if (window.getIdToken) {
      const token = await window.getIdToken();
      if (token) headers['Authorization'] = 'Bearer ' + token;
    }

    const res = await fetch(url, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });

    if (res.status === 401) {
      // Token expired or invalid — send to login.
      sessionStorage.setItem('authRedirect', window.location.href);
      window.location.href = 'login.html';
      return;
    }

    const data = await res.json().catch(() => ({ error: 'non-JSON response' }));

    if (!res.ok) {
      const msg = data.error || `HTTP ${res.status}`;
      showApiError(msg);
      throw new Error(msg);
    }

    return data;
  }

  // Show a brief error toast.
  function showApiError(msg) {
    // Try Alpine toast if available; fall back to console.
    if (window.Alpine) {
      // Dispatch a custom event that pages can listen to.
      window.dispatchEvent(new CustomEvent('api-error', { detail: msg }));
    }
    console.error('[api]', msg);
  }

  // --- Public endpoints ----------------------------------------

  window.Api = {

    // --- Leaderboard / team ---

    getLeaderboard() {
      return apiCall('GET', '/api/leaderboard');
    },

    getTeam(uid) {
      return apiCall('GET', '/api/team/' + encodeURIComponent(uid));
    },

    // --- Players ---

    getPlayers() {
      return apiCall('GET', '/api/players');
    },

    importPlayers(players) {
      return apiCall('POST', '/api/admin/players/import', { players });
    },

    // --- User ---

    register(teamName) {
      return apiCall('POST', '/api/user/register', { teamName });
    },

    updateTeamName(teamName) {
      return apiCall('PUT', '/api/user/team-name', { teamName });
    },

    // --- Users ---

    getUsers() {
      return apiCall('GET', '/api/users');
    },

    // --- Draft ---

    getDraftStatus() {
      return apiCall('GET', '/api/draft/status');
    },

    setDraftOrder(order, randomize = false) {
      return apiCall('POST', '/api/admin/draft/set-order', { order, randomize });
    },

    startDraft() {
      return apiCall('POST', '/api/admin/draft/start');
    },

    pauseDraft() {
      return apiCall('POST', '/api/admin/draft/pause');
    },

    resumeDraft() {
      return apiCall('POST', '/api/admin/draft/resume');
    },

    submitPick(playerId) {
      return apiCall('POST', '/api/draft/pick', { playerId });
    },

    getDraftSuggestions(myRoster, availablePlayers) {
      return apiCall('POST', '/api/draft/suggest', { myRoster, availablePlayers });
    },

    // --- Scores / matches ---

    upsertMatch(matchId, data) {
      return apiCall('PUT', '/api/admin/match/' + encodeURIComponent(matchId), data);
    },

    processScores(matchId, stats) {
      return apiCall('POST', '/api/admin/scores/process', { matchId, stats });
    },
  };
})();
