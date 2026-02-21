// =========================================================
// app.js — Shared Alpine.js stores and mock data
// Phase 1: hardcoded. Replace with API calls in Phase 3.
// =========================================================

document.addEventListener('alpine:init', () => {

  // --- Auth Store -------------------------------------------
  // Defaults: not logged in, loading.
  // auth.js updates this store once Firebase resolves auth state.
  Alpine.store('auth', {
    isLoggedIn:  false,
    isAdmin:     false,
    authLoading: true,  // prevents nav flash — cleared by auth.js
    user:        null,
  });

});

// --- Mock Standings ------------------------------------------
const MOCK_STANDINGS = [
  { uid: 'u1', teamName: 'The Messi Effect',    displayName: 'Andrew', initials: 'AM', colorIdx: 0, totalPoints: 147 },
  { uid: 'u2', teamName: 'Ronaldo Rejects',      displayName: 'Jake',   initials: 'JK', colorIdx: 1, totalPoints: 132 },
  { uid: 'u3', teamName: 'Kane Mutiny',          displayName: 'Sarah',  initials: 'SM', colorIdx: 2, totalPoints: 118 },
  { uid: 'u4', teamName: 'Mbappe Fever',         displayName: 'Tom',    initials: 'TW', colorIdx: 3, totalPoints: 104 },
  { uid: 'u5', teamName: 'Haaland Thunder',      displayName: 'Mike',   initials: 'MB', colorIdx: 4, totalPoints:  97 },
  { uid: 'u6', teamName: "Salah's Army",         displayName: 'Chris',  initials: 'CC', colorIdx: 5, totalPoints:  85 },
  { uid: 'u7', teamName: 'De Bruyne or Bust',    displayName: 'Emma',   initials: 'EP', colorIdx: 6, totalPoints:  71 },
  { uid: 'u8', teamName: 'Neymar Jr FC',         displayName: 'Lisa',   initials: 'LR', colorIdx: 7, totalPoints:  53 },
];

// --- Mock Rosters --------------------------------------------
const MOCK_ROSTERS = {
  u1: {
    uid: 'u1', teamName: 'The Messi Effect', displayName: 'Andrew',
    initials: 'AM', colorIdx: 0, totalPoints: 147, rank: 1,
    players: [
      { name: 'Alisson',           flag: '🇧🇷', country: 'Brazil',      pos: 'GK',  pts: 22 },
      { name: 'Virgil van Dijk',   flag: '🇳🇱', country: 'Netherlands', pos: 'DEF', pts: 18 },
      { name: 'Ruben Dias',        flag: '🇵🇹', country: 'Portugal',    pos: 'DEF', pts: 14 },
      { name: 'Theo Hernandez',    flag: '🇫🇷', country: 'France',      pos: 'DEF', pts: 12 },
      { name: 'Achraf Hakimi',     flag: '🇲🇦', country: 'Morocco',     pos: 'DEF', pts: 16 },
      { name: 'Kevin De Bruyne',   flag: '🇧🇪', country: 'Belgium',     pos: 'MID', pts: 21 },
      { name: 'Jude Bellingham',   flag: '🏴󠁧󠁢󠁥󠁮󠁧󠁿', country: 'England',     pos: 'MID', pts: 19 },
      { name: 'Federico Valverde', flag: '🇺🇾', country: 'Uruguay',     pos: 'MID', pts: 11 },
      { name: 'Pedri',             flag: '🇪🇸', country: 'Spain',       pos: 'MID', pts: 15 },
      { name: 'Kylian Mbappe',     flag: '🇫🇷', country: 'France',      pos: 'FWD', pts: 28 },
      { name: 'Lamine Yamal',      flag: '🇪🇸', country: 'Spain',       pos: 'FWD', pts: 19 },
    ],
  },
  u2: {
    uid: 'u2', teamName: 'Ronaldo Rejects', displayName: 'Jake',
    initials: 'JK', colorIdx: 1, totalPoints: 132, rank: 2,
    players: [
      { name: 'Mike Maignan',      flag: '🇫🇷', country: 'France',      pos: 'GK',  pts: 19 },
      { name: 'Marquinhos',        flag: '🇧🇷', country: 'Brazil',      pos: 'DEF', pts: 17 },
      { name: 'Ronald Araujo',     flag: '🇺🇾', country: 'Uruguay',     pos: 'DEF', pts: 13 },
      { name: 'Dayot Upamecano',   flag: '🇫🇷', country: 'France',      pos: 'DEF', pts: 11 },
      { name: 'Kyle Walker',       flag: '🏴󠁧󠁢󠁥󠁮󠁧󠁿', country: 'England',     pos: 'DEF', pts:  9 },
      { name: 'Toni Kroos',        flag: '🇩🇪', country: 'Germany',     pos: 'MID', pts: 20 },
      { name: 'Luka Modric',       flag: '🇭🇷', country: 'Croatia',     pos: 'MID', pts: 16 },
      { name: 'Bruno Fernandes',   flag: '🇵🇹', country: 'Portugal',    pos: 'MID', pts: 14 },
      { name: 'Declan Rice',       flag: '🏴󠁧󠁢󠁥󠁮󠁧󠁿', country: 'England',     pos: 'MID', pts: 10 },
      { name: 'Harry Kane',        flag: '🏴󠁧󠁢󠁥󠁮󠁧󠁿', country: 'England',     pos: 'FWD', pts: 23 },
      { name: 'Marcus Rashford',   flag: '🏴󠁧󠁢󠁥󠁮󠁧󠁿', country: 'England',     pos: 'FWD', pts: 10 },
    ],
  },
  u3: {
    uid: 'u3', teamName: 'Kane Mutiny', displayName: 'Sarah',
    initials: 'SM', colorIdx: 2, totalPoints: 118, rank: 3,
    players: [
      { name: 'Thibaut Courtois',  flag: '🇧🇪', country: 'Belgium',     pos: 'GK',  pts: 15 },
      { name: 'Antonio Rudiger',   flag: '🇩🇪', country: 'Germany',     pos: 'DEF', pts: 16 },
      { name: 'Jules Kounde',      flag: '🇫🇷', country: 'France',      pos: 'DEF', pts: 12 },
      { name: 'Reece James',       flag: '🏴󠁧󠁢󠁥󠁮󠁧󠁿', country: 'England',     pos: 'DEF', pts:  8 },
      { name: 'Jordi Alba',        flag: '🇪🇸', country: 'Spain',       pos: 'DEF', pts:  7 },
      { name: 'Gavi',              flag: '🇪🇸', country: 'Spain',       pos: 'MID', pts: 18 },
      { name: 'Bernardo Silva',    flag: '🇵🇹', country: 'Portugal',    pos: 'MID', pts: 15 },
      { name: 'Sergej Milinkovic', flag: '🇷🇸', country: 'Serbia',      pos: 'MID', pts: 10 },
      { name: 'Alexis Mac Allister',flag:'🇦🇷', country: 'Argentina',   pos: 'MID', pts: 13 },
      { name: 'Antoine Griezmann', flag: '🇫🇷', country: 'France',      pos: 'FWD', pts: 17 },
      { name: 'Richarlison',       flag: '🇧🇷', country: 'Brazil',      pos: 'FWD', pts:  7 },
    ],
  },
};

// Fallback for users without specific mock data
function getMockRoster(uid) {
  if (MOCK_ROSTERS[uid]) return MOCK_ROSTERS[uid];
  const user = MOCK_STANDINGS.find(u => u.uid === uid);
  if (!user) return null;
  const rank = MOCK_STANDINGS.findIndex(u => u.uid === uid) + 1;
  return {
    ...user, rank,
    players: [
      { name: 'Goalkeeper',     flag: '🌍', country: '—', pos: 'GK',  pts: 14 },
      { name: 'Defender A',     flag: '🌍', country: '—', pos: 'DEF', pts: 11 },
      { name: 'Defender B',     flag: '🌍', country: '—', pos: 'DEF', pts:  9 },
      { name: 'Defender C',     flag: '🌍', country: '—', pos: 'DEF', pts:  8 },
      { name: 'Defender D',     flag: '🌍', country: '—', pos: 'DEF', pts:  6 },
      { name: 'Midfielder A',   flag: '🌍', country: '—', pos: 'MID', pts: 15 },
      { name: 'Midfielder B',   flag: '🌍', country: '—', pos: 'MID', pts: 12 },
      { name: 'Midfielder C',   flag: '🌍', country: '—', pos: 'MID', pts:  9 },
      { name: 'Midfielder D',   flag: '🌍', country: '—', pos: 'MID', pts:  7 },
      { name: 'Forward A',      flag: '🌍', country: '—', pos: 'FWD', pts: 18 },
      { name: 'Forward B',      flag: '🌍', country: '—', pos: 'FWD', pts: 10 },
    ],
  };
}

// --- Mock Draft Data -----------------------------------------
const MOCK_AVAILABLE_PLAYERS = [
  { id: 'pa1',  name: 'Marc-Andre ter Stegen', flag: '🇩🇪', country: 'Germany',   pos: 'GK',  rating: 88 },
  { id: 'pa2',  name: 'Jordan Pickford',        flag: '🏴󠁧󠁢󠁥󠁮󠁧󠁿', country: 'England',   pos: 'GK',  rating: 82 },
  { id: 'pa3',  name: 'Unai Simon',             flag: '🇪🇸', country: 'Spain',     pos: 'GK',  rating: 81 },
  { id: 'pa4',  name: 'Keylor Navas',           flag: '🇨🇷', country: 'Costa Rica',pos: 'GK',  rating: 79 },
  { id: 'pa5',  name: 'Wan-Bissaka',            flag: '🏴󠁧󠁢󠁥󠁮󠁧󠁿', country: 'England',   pos: 'DEF', rating: 80 },
  { id: 'pa6',  name: 'Benjamin Pavard',        flag: '🇫🇷', country: 'France',    pos: 'DEF', rating: 83 },
  { id: 'pa7',  name: 'Joao Cancelo',           flag: '🇵🇹', country: 'Portugal',  pos: 'DEF', rating: 85 },
  { id: 'pa8',  name: 'Eder Militao',           flag: '🇧🇷', country: 'Brazil',    pos: 'DEF', rating: 84 },
  { id: 'pa9',  name: 'Gavi',                   flag: '🇪🇸', country: 'Spain',     pos: 'MID', rating: 87 },
  { id: 'pa10', name: 'Luka Modric',            flag: '🇭🇷', country: 'Croatia',   pos: 'MID', rating: 87 },
  { id: 'pa11', name: 'Bruno Fernandes',        flag: '🇵🇹', country: 'Portugal',  pos: 'MID', rating: 86 },
  { id: 'pa12', name: 'Declan Rice',            flag: '🏴󠁧󠁢󠁥󠁮󠁧󠁿', country: 'England',   pos: 'MID', rating: 84 },
  { id: 'pa13', name: 'Alexis Mac Allister',    flag: '🇦🇷', country: 'Argentina', pos: 'MID', rating: 83 },
  { id: 'pa14', name: 'Erling Haaland',         flag: '🇳🇴', country: 'Norway',    pos: 'FWD', rating: 95 },
  { id: 'pa15', name: 'Vinicius Jr',            flag: '🇧🇷', country: 'Brazil',    pos: 'FWD', rating: 92 },
  { id: 'pa16', name: 'Antoine Griezmann',      flag: '🇫🇷', country: 'France',    pos: 'FWD', rating: 86 },
  { id: 'pa17', name: 'Gabriel Jesus',          flag: '🇧🇷', country: 'Brazil',    pos: 'FWD', rating: 82 },
  { id: 'pa18', name: 'Son Heung-min',          flag: '🇰🇷', country: 'South Korea',pos:'FWD', rating: 88 },
];

const MOCK_DRAFT_HISTORY = [
  { pick: 1,  round: 1, name: 'Andrew', player: 'Kylian Mbappe',          flag: '🇫🇷', pos: 'FWD', colorIdx: 0 },
  { pick: 2,  round: 1, name: 'Jake',   player: 'Jude Bellingham',        flag: '🏴󠁧󠁢󠁥󠁮󠁧󠁿', pos: 'MID', colorIdx: 1 },
  { pick: 3,  round: 1, name: 'Sarah',  player: 'Kevin De Bruyne',        flag: '🇧🇪', pos: 'MID', colorIdx: 2 },
  { pick: 4,  round: 1, name: 'Tom',    player: 'Mohamed Salah',          flag: '🇪🇬', pos: 'FWD', colorIdx: 3 },
  { pick: 5,  round: 1, name: 'Mike',   player: 'Harry Kane',             flag: '🏴󠁧󠁢󠁥󠁮󠁧󠁿', pos: 'FWD', colorIdx: 4 },
  { pick: 6,  round: 1, name: 'Chris',  player: 'Pedri',                  flag: '🇪🇸', pos: 'MID', colorIdx: 5 },
  { pick: 7,  round: 1, name: 'Emma',   player: 'Vinicius Jr',            flag: '🇧🇷', pos: 'FWD', colorIdx: 6 },
  { pick: 8,  round: 1, name: 'Lisa',   player: 'Robert Lewandowski',     flag: '🇵🇱', pos: 'FWD', colorIdx: 7 },
  { pick: 9,  round: 2, name: 'Lisa',   player: 'Alisson',                flag: '🇧🇷', pos: 'GK',  colorIdx: 7 },
  { pick: 10, round: 2, name: 'Emma',   player: 'Thibaut Courtois',       flag: '🇧🇪', pos: 'GK',  colorIdx: 6 },
  { pick: 11, round: 2, name: 'Chris',  player: 'Federico Valverde',      flag: '🇺🇾', pos: 'MID', colorIdx: 5 },
  { pick: 12, round: 2, name: 'Tom',    player: 'Lamine Yamal',           flag: '🇪🇸', pos: 'FWD', colorIdx: 3 },
];
