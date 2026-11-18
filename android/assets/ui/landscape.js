// OpenPlayer theme landscapes: original vector artwork (mountains, aurora,
// lake with reflections), generated locally so it works offline and stays
// sharp at any size and display scale.
(function () {
  const P = {
    "black-gold":   { sky: ["#0b0a08", "#2b1d0c", "#b98a3e"], sun: "#f5d78e", aur: ["#be872d", "#f5d78e"], m: ["#3a2e22", "#241c14", "#120e0a"], lake: ["#2a2116", "#070707"], shore: "#050505", snow: "#f5d78e" },
    "aurora-night": { sky: ["#060b18", "#14204a", "#3b2f7a"], sun: "#a5b4fc", aur: ["#6d4aff", "#06b6d4"], m: ["#1e2a52", "#141d3d", "#0a1128"], lake: ["#1b2550", "#060b18"], shore: "#04070f", snow: "#c7d2fe" },
    "honey":        { sky: ["#fff1c7", "#fbd66b", "#f6b93b"], sun: "#fffaf0", aur: ["#f6b93b", "#ffe9a8"], m: ["#d79a4a", "#b8732f", "#8a5223"], lake: ["#5fc6c2", "#2e8f95"], shore: "#5a3518", snow: "#fff6dc" },
    "burgundy":     { sky: ["#2a0f1d", "#6f173b", "#d48ba4"], sun: "#ffd1dc", aur: ["#a32d56", "#d48ba4"], m: ["#5a2340", "#3d1530", "#240b1b"], lake: ["#4a1a33", "#1c0812"], shore: "#12050b", snow: "#f6c6d4" },
    "aurora-glass": { sky: ["#f5f3ff", "#dbeafe", "#c7d2fe"], sun: "#ffffff", aur: ["#a78bfa", "#67e8f9"], m: ["#b8b5e8", "#9a97d6", "#7c7bc0"], lake: ["#a5f3fc", "#7aa7f5"], shore: "#4c4a8a", snow: "#ffffff" },
    "retro-white":  { sky: ["#fbfaf6", "#ece7dc", "#d9d2c3"], sun: "#ffffff", aur: ["#c8102e", "#111111"], m: ["#a9a397", "#7d776c", "#4a463f"], lake: ["#d9d4c8", "#9b958a"], shore: "#2a2824", snow: "#ffffff" },
    "oled-black":   { sky: ["#000000", "#05080a", "#0d2a30"], sun: "#67d4e8", aur: ["#0e3a44", "#67d4e8"], m: ["#0d1416", "#080c0d", "#030505"], lake: ["#081215", "#000000"], shore: "#000000", snow: "#3a6c75" },
  };
  P["midnight-glass"] = P["aurora-night"];
  P["aurora"] = { ...P["aurora-night"], sky: ["#0c0a18", "#251c45", "#4a3a7a"], aur: ["#c4b5fd", "#67e8f9"] };
  P["frost"] = { ...P["aurora-glass"], aur: ["#93c5fd", "#5eead4"] };

  // Deterministic ridge so the art is identical on every start.
  function ridge(seed, base, amp, step, w) {
    let x = 0, pts = [], s = seed;
    const rnd = () => ((s = (s * 9301 + 49297) % 233280) / 233280);
    while (x <= w + step) { pts.push([x, base - amp * (0.35 + 0.65 * Math.abs(Math.sin(x / (w / 2.3) + seed)) ) * (0.75 + 0.5 * rnd())]); x += step * (0.6 + rnd() * 0.8); }
    return pts;
  }
  const path = (pts, bottom) => "M0," + bottom + " " + pts.map((p) => "L" + p[0].toFixed(1) + "," + p[1].toFixed(1)).join(" ") + " L1600," + bottom + " Z";

  window.landscapeSVG = function (id) {
    const c = P[id];
    if (!c) return ""; // High Contrast: no decoration
    const far = ridge(3, 560, 230, 70, 1600), mid = ridge(7, 600, 170, 55, 1600), near = ridge(11, 650, 120, 40, 1600);
    const horizon = 650;
    return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1600 900" preserveAspectRatio="xMidYMid slice" aria-hidden="true">
  <defs>
    <linearGradient id="sky" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${c.sky[0]}"/><stop offset=".55" stop-color="${c.sky[1]}"/><stop offset="1" stop-color="${c.sky[2]}"/></linearGradient>
    <radialGradient id="sun" cx="1080" cy="560" r="420" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="${c.sun}" stop-opacity=".95"/><stop offset=".12" stop-color="${c.sun}" stop-opacity=".55"/><stop offset="1" stop-color="${c.sun}" stop-opacity="0"/></radialGradient>
    <linearGradient id="aur" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="${c.aur[0]}" stop-opacity="0"/><stop offset=".35" stop-color="${c.aur[0]}"/><stop offset=".7" stop-color="${c.aur[1]}"/><stop offset="1" stop-color="${c.aur[1]}" stop-opacity="0"/></linearGradient>
    <linearGradient id="lake" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${c.lake[0]}"/><stop offset="1" stop-color="${c.lake[1]}"/></linearGradient>
    <linearGradient id="haze" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${c.sky[2]}" stop-opacity="0"/><stop offset="1" stop-color="${c.sky[2]}" stop-opacity=".55"/></linearGradient>
    <linearGradient id="streak" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="${c.sun}" stop-opacity=".75"/><stop offset="1" stop-color="${c.sun}" stop-opacity="0"/></linearGradient>
    <filter id="soft" x="-20%" y="-50%" width="140%" height="200%"><feGaussianBlur stdDeviation="28"/></filter>
    <filter id="ripple" x="0" y="0" width="100%" height="100%"><feGaussianBlur stdDeviation="3 1.5"/></filter>
  </defs>
  <rect width="1600" height="900" fill="url(#sky)"/>
  <g filter="url(#soft)" opacity=".6">
    <path d="M-50,260 C250,140 520,330 820,210 S1300,120 1650,250 L1650,330 C1300,220 1080,300 820,300 S300,250 -50,350 Z" fill="url(#aur)"/>
    <path d="M100,150 C420,60 700,230 1000,120 S1450,90 1700,170 L1700,210 C1400,150 1150,200 980,190 S450,140 100,220 Z" fill="url(#aur)" opacity=".7"/>
  </g>
  <rect width="1600" height="900" fill="url(#sun)"/>
  <circle cx="1080" cy="505" r="30" fill="${c.sun}" opacity=".9"/>
  <path d="${path(far, horizon)}" fill="${c.m[0]}"/>
  <path d="${path(far, horizon)}" fill="url(#haze)"/>
  <path d="${path(mid, horizon)}" fill="${c.m[1]}"/>
  <path d="${path(near, horizon)}" fill="${c.m[2]}"/>
  <path d="${far.slice(8, 15).map((p, i) => (i ? "L" : "M") + p[0].toFixed(1) + "," + (p[1] + 2).toFixed(1)).join(" ")}" fill="none" stroke="${c.snow}" stroke-opacity=".45" stroke-width="3" stroke-linejoin="round"/>
  <rect y="${horizon}" width="1600" height="${900 - horizon}" fill="url(#lake)"/>
  <g transform="translate(0 ${2 * horizon}) scale(1 -1)" opacity=".38" filter="url(#ripple)">
    <path d="${path(far, horizon)}" fill="${c.m[0]}"/><path d="${path(mid, horizon)}" fill="${c.m[1]}"/><path d="${path(near, horizon)}" fill="${c.m[2]}"/>
  </g>
  <rect x="1060" y="${horizon}" width="40" height="190" rx="20" fill="url(#streak)" opacity=".5" filter="url(#ripple)"/>
  <g stroke="${c.sun}" stroke-opacity=".22" stroke-width="2" stroke-linecap="round">
    <path d="M920 ${horizon + 40}h90M1120 ${horizon + 62}h120M980 ${horizon + 95}h70M1150 ${horizon + 130}h60M860 ${horizon + 150}h80"/>
  </g>
  <path d="M0,900 L0,822 C180,792 330,836 520,814 C700,792 860,846 1020,848 C1180,850 1300,880 1420,900 Z" fill="${c.shore}" opacity=".9"/>
</svg>`;
  };
})();
