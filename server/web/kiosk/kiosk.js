(() => {
  "use strict";

  const $ = (sel) => document.querySelector(sel);
  const $$ = (sel) => Array.from(document.querySelectorAll(sel));

  const state = {
    photos: [],
    albums: [],
    publicCategories: [],
    counts: { family: 0, public: 0, favorites: 0, hidden: 0 },
    settings: {
      mode: "family",
      album: "",
      intervalSec: 20,
      order: "shuffle",
      transition: "kenburns",
      fitMode: "ambient",
      showClock: true,
      clock24h: false,
      showCaption: true,
      showDateBadge: true,
      publicSource: "all",
      publicCategory: "all",
      brightness: 200,
      sleepEnabled: false,
      sleepStart: "22:00",
      sleepEnd: "07:00",
      familyToken: "",
      frameName: "The Jimmy Frame",
      notice: null,
    },
    stats: null,
    index: 0,
    activeLayer: 0, // 0 => layer-a, 1 => layer-b
    paused: false,
    hudOpen: false,
    sleeping: false,
    manualWakeUntil: 0,
    slideTimer: null,
    hudTimer: null,
    toastTimer: null,
    cacheBust: {},
  };

  // Native Android WebView bridge adapter
  const Bridge = {
    isNative() {
      return !!(window.FrameBridge && typeof window.FrameBridge.isNative === "function" && window.FrameBridge.isNative());
    },
    setBrightness(level) {
      const clamped = Math.max(0, Math.min(255, Number(level) || 200));
      if (this.isNative() && window.FrameBridge.setBrightness) {
        window.FrameBridge.setBrightness(clamped);
        $("#sim-dimmer").style.opacity = "0";
      } else {
        // Smooth simulated dim overlay for desktop dev preview
        const dimOpacity = Math.max(0, (255 - clamped) / 255) * 0.72;
        $("#sim-dimmer").style.opacity = dimOpacity.toFixed(2);
      }
    },
    setScreenSleep(asleep) {
      if (this.isNative() && window.FrameBridge.setScreenSleep) {
        window.FrameBridge.setScreenSleep(!!asleep);
      }
    },
    getDeviceInfo() {
      if (this.isNative() && window.FrameBridge.getDeviceInfo) {
        try {
          return JSON.parse(window.FrameBridge.getDeviceInfo());
        } catch (_) {}
      }
      return null;
    },
    openWifiSettings() {
      if (this.isNative() && window.FrameBridge.openWifiSettings) {
        window.FrameBridge.openWifiSettings();
      } else {
        showToast("Wi-Fi Settings opens natively on the Android frame.");
      }
    },
    launchApp(pkg, label) {
      if (this.isNative() && window.FrameBridge.launchApp) {
        const ok = window.FrameBridge.launchApp(pkg);
        if (!ok) showToast(`${label} is not installed on this frame.`);
      } else {
        showToast(`${label} launches natively on the Android frame.`);
      }
    },
    rebootDevice() {
      if (this.isNative() && window.FrameBridge.rebootDevice) {
        window.FrameBridge.rebootDevice();
      } else {
        showToast("Reboot runs natively on the Android frame.");
      }
    },
  };

  // API helpers
  async function apiJSON(url, opts = {}) {
    const res = await fetch(url, {
      headers: { "Content-Type": "application/json", ...(opts.headers || {}) },
      ...opts,
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res.json();
  }

  async function patchSettings(patch) {
    try {
      const updated = await apiJSON("/api/settings", {
        method: "PATCH",
        body: JSON.stringify(patch),
      });
      state.settings = { ...state.settings, ...updated };
      applySettingsVisuals();
      syncSettingsUI();
    } catch (err) {
      showToast("Could not save setting");
    }
  }

  // Initialization
  async function init() {
    bindUI();
    bindGestures();
    startClock();
    await loadSlideshow(true);
    await loadSystemStats();
    connectSSE();
    setInterval(() => {
      checkSleepSchedule();
      renderNotice();
    }, 15000);
    setInterval(loadSystemStats, 60000);
  }

  async function loadSlideshow(resetIndex = false) {
    try {
      const data = await apiJSON("/api/slideshow");
      state.settings = { ...state.settings, ...(data.settings || {}) };
      state.photos = data.photos || [];
      state.albums = data.albums || [];
      state.publicCategories = data.publicCategories || [];
      state.counts = data.counts || state.counts;

      applySettingsVisuals();
      syncHUDControls();
      syncSettingsUI();
      updatePublicStatus(data.publicStatus);

      if (state.photos.length === 0) {
        showEmptyState();
      } else {
        hideEmptyState();
        if (resetIndex || state.index >= state.photos.length) {
          state.index = 0;
        }
        renderSlide(state.index, "fade");
        resetSlideTimer();
      }
    } catch (err) {
      console.error("Failed to load slideshow:", err);
      showToast("Waiting for frame server...");
    }
  }

  async function loadSystemStats() {
    try {
      const stats = await apiJSON("/api/system/stats");
      state.stats = stats;
      renderStatsUI();
    } catch (_) {}
  }

  function currentPhoto() {
    return state.photos[state.index] || null;
  }

  function photoURL(photo, kind = "display") {
    const bust = state.cacheBust[photo.id] ? `?v=${state.cacheBust[photo.id]}` : "";
    return `/api/photos/${photo.id}/${kind}${bust}`;
  }

  function renderSlide(idx, dir = "fade") {
    const photo = state.photos[idx];
    if (!photo) return;

    const layers = [$("#layer-a"), $("#layer-b")];
    const nextIdx = state.activeLayer === 0 ? 1 : 0;
    const outLayer = layers[state.activeLayer];
    const inLayer = layers[nextIdx];

    const url = photoURL(photo, "display");
    const backdrop = inLayer.querySelector(".backdrop");
    const img = inLayer.querySelector(".photo");

    // Reset animation for Ken Burns
    img.style.animation = "none";
    void img.offsetWidth;
    img.style.animation = "";

    inLayer.classList.remove("slide-in-right", "slide-in-left");
    if (state.settings.transition === "slide" && dir !== "fade") {
      inLayer.classList.add(dir === "next" ? "slide-in-right" : "slide-in-left");
    }

    const commit = () => {
      backdrop.src = url;
      img.src = url;
      img.alt = photoTitle(photo);

      requestAnimationFrame(() => {
        inLayer.classList.remove("slide-in-right", "slide-in-left");
        inLayer.classList.add("active");
        outLayer.classList.remove("active");
        state.activeLayer = nextIdx;
      });
    };

    const preloader = new Image();
    preloader.onload = commit;
    preloader.onerror = commit;
    preloader.src = url;

    // Preload upcoming photo in background
    if (state.photos.length > 1) {
      const upcoming = state.photos[(idx + 1) % state.photos.length];
      if (upcoming) {
        const nextPreload = new Image();
        nextPreload.src = photoURL(upcoming, "display");
      }
    }

    updatePhotoOverlays(photo);
    updateFavoriteButton(photo);
    if (!$("#info-drawer").classList.contains("hidden")) {
      renderInfoDrawer(photo);
    }
  }

  function photoTitle(p) {
    if (p.caption) return p.caption;
    if (p.publicMeta && p.publicMeta.title) return p.publicMeta.title;
    return "";
  }

  function updatePhotoOverlays(p) {
    $("#photo-counter").textContent = `${state.index + 1} / ${state.photos.length}`;

    const capEl = $("#ambient-caption");
    const title = photoTitle(p);
    const subParts = [];

    if (p.source === "public" && p.publicMeta) {
      if (p.publicMeta.artist) subParts.push(p.publicMeta.artist);
      if (state.settings.showDateBadge && p.publicMeta.date) subParts.push(p.publicMeta.date);
    } else {
      if (p.sender) subParts.push(`From ${p.sender}`);
      if (state.settings.showDateBadge) {
        if (p.exif && p.exif.locationName) subParts.push(p.exif.locationName);
        const dt = formatShortDate(p.exif && p.exif.takenAt ? p.exif.takenAt : p.uploadedAt);
        if (dt) subParts.push(dt);
      }
    }

    const showCap = state.settings.showCaption && (title || subParts.length > 0);
    if (!showCap) {
      capEl.classList.add("hidden");
    } else {
      $("#cap-title").textContent = title;
      $("#cap-title").classList.toggle("hidden", !title);
      $("#cap-sub").textContent = subParts.join(" · ");
      $("#cap-sub").classList.toggle("hidden", subParts.length === 0);
      capEl.classList.remove("hidden");
    }
  }

  function updateFavoriteButton(p) {
    const fav = !!(p && p.favorite);
    $("#btn-fav").classList.toggle("is-fav", fav);
    $("#info-fav").textContent = fav ? "♥ Favorited" : "♡ Favorite";
  }

  function nextSlide(manual = false) {
    if (state.photos.length <= 1) return;
    state.index = (state.index + 1) % state.photos.length;
    renderSlide(state.index, "next");
    if (manual) resetSlideTimer();
  }

  function prevSlide(manual = false) {
    if (state.photos.length <= 1) return;
    state.index = (state.index - 1 + state.photos.length) % state.photos.length;
    renderSlide(state.index, "prev");
    if (manual) resetSlideTimer();
  }

  function resetSlideTimer() {
    clearInterval(state.slideTimer);
    if (state.paused || state.sleeping || state.photos.length <= 1) return;
    const ms = Math.max(5, state.settings.intervalSec || 20) * 1000;
    state.slideTimer = setInterval(() => nextSlide(false), ms);
  }

  function setPaused(val) {
    state.paused = val;
    $("#icon-pause").classList.toggle("hidden", val);
    $("#icon-play").classList.toggle("hidden", !val);
    $("#label-play").textContent = val ? "Play" : "Pause";
    resetSlideTimer();
  }

  // Empty State
  function showEmptyState() {
    clearInterval(state.slideTimer);
    $("#empty-state").classList.remove("hidden");
    $("#ambient-caption").classList.add("hidden");
    $("#photo-counter").textContent = "0 / 0";

    const mode = state.settings.mode;
    if (mode === "public") {
      $("#empty-eyebrow").textContent = "Public Gallery";
      $("#empty-title").textContent = "Fetching curated art & photography...";
      $("#empty-desc").textContent = "Downloading museum masterpieces, Wikimedia featured photography, and NASA space imagery to local cache. Make sure the frame is on Wi-Fi.";
      $("#empty-public-btn").textContent = "Refresh Public Feed";
    } else {
      $("#empty-eyebrow").textContent = `Welcome to ${state.settings.frameName || "The Jimmy Frame"}`;
      $("#empty-title").textContent = "Ready for your first family photos";
      $("#empty-desc").textContent = "Scan this QR code from any phone on your Wi-Fi to send family photos straight to this frame — no accounts, 100% local.";
      $("#empty-public-btn").textContent = "Explore Public Gallery";
    }
    $("#empty-qr").src = `/api/qr.svg?t=${Date.now()}`;
    $("#empty-token").textContent = `Token: ${state.settings.familyToken || "..."}`;
  }

  function hideEmptyState() {
    $("#empty-state").classList.add("hidden");
  }

  // HUD visibility
  function showHUD() {
    state.hudOpen = true;
    $("#stage").classList.add("hud-open");
    $("#hud").classList.remove("hidden");
    bumpHUDTimer();
  }

  function hideHUD() {
    state.hudOpen = false;
    $("#stage").classList.remove("hud-open");
    $("#hud").classList.add("hidden");
    clearTimeout(state.hudTimer);
  }

  function bumpHUDTimer() {
    clearTimeout(state.hudTimer);
    if (!state.hudOpen) return;
    if (!$("#info-drawer").classList.contains("hidden") || !$("#settings-modal").classList.contains("hidden")) {
      return;
    }
    state.hudTimer = setTimeout(hideHUD, 8000);
  }

  // Apply settings visuals
  function applySettingsVisuals() {
    const st = state.settings;
    const stage = $("#stage");
    stage.classList.remove("fit-ambient", "fit-fill", "fit-matte", "transition-kenburns", "transition-fade", "transition-slide");
    stage.classList.add(`fit-${st.fitMode || "ambient"}`);
    stage.classList.add(`transition-${st.transition || "kenburns"}`);

    $("#ambient-clock").classList.toggle("hidden", !st.showClock);

    const fitLabels = { ambient: "Ambient", fill: "Fill", matte: "Matte" };
    $("#label-fit").textContent = fitLabels[st.fitMode] || "Ambient";

    Bridge.setBrightness(st.brightness);
    updateClock();
    renderNotice();
    const p = currentPhoto();
    if (p) updatePhotoOverlays(p);
    resetSlideTimer();
  }

  function renderNotice() {
    const el = $("#frame-notice");
    if (!el) return;
    const n = state.settings.notice;
    const expired = n && n.expiresAt && new Date(n.expiresAt).getTime() <= Date.now();
    if (!n || !n.text || !n.text.trim() || expired) {
      el.classList.add("hidden");
      return;
    }
    el.classList.remove("tone-ocean", "tone-teal", "tone-sun");
    const tone = ["ocean", "teal", "sun"].includes(n.tone) ? n.tone : "ocean";
    el.classList.add(`tone-${tone}`);
    $("#notice-text").textContent = n.text.trim();
    const fromEl = $("#notice-from");
    if (n.from && n.from.trim()) {
      fromEl.textContent = `From ${n.from.trim()}`;
      fromEl.classList.remove("hidden");
    } else {
      fromEl.classList.add("hidden");
    }
    el.classList.remove("hidden");
  }

  function syncHUDControls() {
    const st = state.settings;
    $$(".mode-btn").forEach((btn) => {
      btn.classList.toggle("active", btn.dataset.mode === st.mode);
    });
    $("#count-family").textContent = state.counts.family || 0;
    $("#count-public").textContent = state.counts.public || 0;

    const albumSel = $("#album-select");
    const showAlbum = st.mode === "family";
    albumSel.classList.toggle("hidden", !showAlbum);

    const existing = new Set(["", "favorites"]);
    albumSel.innerHTML = `<option value="">All Family</option><option value="favorites">♥ Favorites (${state.counts.favorites || 0})</option>`;
    state.albums.forEach((a) => {
      if (existing.has(a)) return;
      const opt = document.createElement("option");
      opt.value = a;
      opt.textContent = a;
      albumSel.appendChild(opt);
    });
    albumSel.value = st.album || "";

    const pubSel = $("#pub-cat-select");
    const showPubCat = st.mode === "public" || st.mode === "mix";
    pubSel.classList.toggle("hidden", !showPubCat);

    const builtins = [
      ["all", "All Curated"],
      ["fine_art", "Fine Art"],
      ["nature", "Nature & Wildlife"],
      ["space", "Space & Cosmos"],
      ["architecture", "Architecture"],
      ["vintage", "Vintage & History"],
    ];
    const seen = new Set(builtins.map(([id]) => id));
    pubSel.innerHTML = builtins.map(([id, label]) => `<option value="${id}">${label}</option>`).join("");
    state.publicCategories.forEach((cat) => {
      if (seen.has(cat)) return;
      seen.add(cat);
      const opt = document.createElement("option");
      opt.value = cat;
      opt.textContent = cat.startsWith("search:") ? `🔍 ${cat.slice(7)}` : cat;
      pubSel.appendChild(opt);
    });
    const activeCat = st.publicCategory || st.publicSource || "all";
    if (!seen.has(activeCat)) {
      const opt = document.createElement("option");
      opt.value = activeCat;
      opt.textContent = activeCat.startsWith("search:") ? `🔍 ${activeCat.slice(7)}` : activeCat;
      pubSel.appendChild(opt);
    }
    pubSel.value = activeCat;
  }

  function syncSettingsUI() {
    const st = state.settings;
    $$(".chip-row").forEach((row) => {
      const key = row.dataset.setting;
      const cur = String(st[key]);
      row.querySelectorAll(".opt-chip").forEach((btn) => {
        btn.classList.toggle("active", btn.dataset.val === cur);
      });
    });

    $("#set-showClock").checked = !!st.showClock;
    $("#set-clock24h").checked = !!st.clock24h;
    $("#set-showCaption").checked = !!st.showCaption;
    $("#set-showDateBadge").checked = !!st.showDateBadge;
    $("#set-brightness").value = st.brightness ?? 200;
    $("#brightness-pct").textContent = `${Math.round(((st.brightness ?? 200) / 255) * 100)}%`;
    $("#set-sleepEnabled").checked = !!st.sleepEnabled;
    $("#set-sleepStart").value = st.sleepStart || "22:00";
    $("#set-sleepEnd").value = st.sleepEnd || "07:00";
    $("#qr-token").textContent = st.familyToken || "—";
  }

  function renderStatsUI() {
    const s = state.stats;
    if (!s) return;

    $("#empty-url").textContent = s.friendlyUrl || s.manageUrl;
    $("#qr-friendly-url").textContent = s.friendlyUrl || "http://frame.local:8080/manage";
    $("#qr-lan-url").textContent = s.lanIp ? `http://${s.lanIp}:${s.port}/manage` : "Not connected";
    if (s.tailscaleIp) {
      $("#qr-ts-row").classList.remove("hidden");
      $("#qr-ts-url").textContent = `http://${s.tailscaleIp}:${s.port}/manage`;
    }

    if (s.totalBytes > 0) {
      const usedGB = (s.usedBytes / 1e9).toFixed(1);
      const totalGB = (s.totalBytes / 1e9).toFixed(1);
      const freeGB = (s.freeBytes / 1e9).toFixed(1);
      const pct = Math.min(100, Math.round((s.usedBytes / s.totalBytes) * 100));
      $("#storage-text").textContent = `${freeGB} GB free of ${totalGB} GB`;
      $("#storage-bar").style.width = `${pct}%`;
      $("#library-breakdown").textContent = `${usedGB} GB used · ${s.counts.family} family photos · ${s.counts.public} public cached · ${s.counts.favorites} favorites`;
    }

    updatePublicStatus(s.publicStatus);

    const nativeInfo = Bridge.getDeviceInfo();
    const parts = [`frmaeo v${s.version}`, `Host: ${s.hostname || "frame"}`];
    if (nativeInfo && nativeInfo.androidVersion) {
      parts.push(`Android ${nativeInfo.androidVersion}`);
    }
    if (s.uptimeSec != null) {
      const mins = Math.floor(s.uptimeSec / 60);
      parts.push(mins >= 60 ? `Uptime ${Math.floor(mins / 60)}h ${mins % 60}m` : `Uptime ${mins}m`);
    }
    $("#device-sys-info").textContent = parts.join(" · ");
  }

  function updatePublicStatus(ps) {
    if (!ps) return;
    const el = $("#pub-status-text");
    const btn = $("#btn-refresh-public");
    const searchBtn = $("#btn-pub-search");
    const pWrap = $("#pub-progress-wrap");
    const pBar = $("#pub-progress-bar");
    const pItem = $("#pub-progress-item");
    const pPct = $("#pub-progress-pct");

    const eWrap = $("#empty-progress");
    const eBar = $("#empty-progress-bar");
    const eLabel = $("#empty-progress-label");
    const ePct = $("#empty-progress-pct");

    const pct = Math.max(0, Math.min(100, Number(ps.percent) || 0));

    if (ps.refreshing) {
      const countText = ps.total > 0 ? `${ps.done} / ${ps.total}` : `${ps.cached} cached`;
      el.textContent = `Downloading (${countText})…`;
      btn.disabled = true;
      btn.textContent = "Downloading…";
      if (searchBtn) searchBtn.disabled = true;

      pWrap.classList.remove("hidden");
      pBar.style.width = `${Math.max(6, pct)}%`;
      pItem.textContent = ps.currentItem || "Downloading public images…";
      pPct.textContent = `${pct}%`;

      if (state.settings.mode === "public") {
        eWrap.classList.remove("hidden");
        eBar.style.width = `${Math.max(6, pct)}%`;
        eLabel.textContent = ps.currentItem || `Downloading (${countText})…`;
        ePct.textContent = `${pct}%`;
      }
    } else {
      btn.disabled = false;
      btn.textContent = "Refresh Category";
      if (searchBtn) searchBtn.disabled = false;
      pWrap.classList.add("hidden");
      eWrap.classList.add("hidden");

      let txt = `${ps.cached} photos cached locally`;
      if (ps.lastError) txt += ` · ${ps.lastError}`;
      el.textContent = txt;
    }
  }

  // Photo Details & EXIF Drawer
  function renderInfoDrawer(p) {
    if (!p) return;
    const isPub = p.source === "public";
    const providerLabels = {
      met: "The Met Museum",
      wikimedia: "Wikimedia Featured",
      nasa: "NASA Image Library",
    };

    $("#info-badge").textContent = isPub
      ? providerLabels[p.publicMeta?.provider] || "Public Gallery"
      : p.album ? `Family · ${p.album}` : "Family Photo";

    const title = photoTitle(p) || p.filename || "Untitled Photo";
    $("#info-title").textContent = title;

    if (isPub && p.publicMeta) {
      $("#info-subtitle").textContent = [p.publicMeta.artist, p.publicMeta.date].filter(Boolean).join(" · ");
      const desc = p.publicMeta.description || "";
      $("#info-description").textContent = desc;
      $("#info-description").classList.toggle("hidden", !desc);
    } else {
      $("#info-subtitle").textContent = p.sender ? `Shared by ${p.sender}` : "Stored locally on frame";
      $("#info-description").classList.add("hidden");
    }

    const tiles = [];
    const exif = p.exif || {};
    const taken = exif.takenAt ? formatFullDate(exif.takenAt) : formatFullDate(p.uploadedAt);
    if (taken) tiles.push({ k: exif.takenAt ? "Date Taken" : "Added to Frame", v: taken });

    if (exif.hasGps && exif.locationName) {
      tiles.push({ k: "Location", v: exif.locationName });
    }
    const cam = [exif.cameraMake, exif.cameraModel].filter(Boolean).join(" ").replace(/^(\S+)\s+\1/i, "$1");
    if (cam) tiles.push({ k: "Camera", v: cam });
    if (exif.lens) tiles.push({ k: "Lens", v: exif.lens });
    if (p.width && p.height) {
      const mp = ((p.width * p.height) / 1e6).toFixed(1);
      tiles.push({ k: "Resolution", v: `${p.width} × ${p.height} (${mp} MP)` });
    }

    $("#info-meta-grid").innerHTML = tiles
      .map((t) => `<div class="meta-tile"><div class="k">${esc(t.k)}</div><div class="v">${esc(t.v)}</div></div>`)
      .join("");

    const chips = [];
    if (exif.focalLength) chips.push(exif.focalLength);
    if (exif.aperture) chips.push(exif.aperture);
    if (exif.shutter) chips.push(exif.shutter);
    if (exif.iso) chips.push(`ISO ${exif.iso}`);
    if (exif.hasGps) chips.push(`${exif.lat.toFixed(3)}, ${exif.lon.toFixed(3)}`);
    if (p.sizeBytes) chips.push(`${(p.sizeBytes / (1024 * 1024)).toFixed(1)} MB`);

    $("#info-exif-chips").innerHTML = chips.map((c) => `<span class="exif-chip">${esc(c)}</span>`).join("");
    updateFavoriteButton(p);
  }

  function openModal(id) {
    $(`#${id}`).classList.remove("hidden");
    clearTimeout(state.hudTimer);
    if (id === "info-drawer") {
      renderInfoDrawer(currentPhoto());
    } else if (id === "settings-modal") {
      $("#settings-qr").src = `/api/qr.svg?t=${Date.now()}`;
      loadSystemStats();
    }
  }

  function closeModal(id) {
    $(`#${id}`).classList.add("hidden");
    bumpHUDTimer();
  }

  // Clock & Night Sleep Schedule
  function startClock() {
    updateClock();
    setInterval(updateClock, 10000);
  }

  function updateClock() {
    const now = new Date();
    const h24 = now.getHours();
    const m = String(now.getMinutes()).padStart(2, "0");
    let timeStr;
    if (state.settings.clock24h) {
      timeStr = `${String(h24).padStart(2, "0")}:${m}`;
    } else {
      const h12 = h24 % 12 || 12;
      const ampm = h24 >= 12 ? "PM" : "AM";
      timeStr = `${h12}:${m} ${ampm}`;
    }
    const dateStr = now.toLocaleDateString(undefined, { weekday: "short", month: "short", day: "numeric" });
    $("#clock-time").textContent = timeStr;
    $("#clock-date").textContent = dateStr;
    $("#sleep-clock").textContent = timeStr.replace(/\s?(AM|PM)$/, "");
  }

  function enterSleep() {
    state.sleeping = true;
    hideHUD();
    closeModal("info-drawer");
    closeModal("settings-modal");
    $("#sleep-overlay").classList.remove("hidden");
    Bridge.setScreenSleep(true);
    clearInterval(state.slideTimer);
  }

  function exitSleep(manual = false) {
    state.sleeping = false;
    $("#sleep-overlay").classList.add("hidden");
    Bridge.setScreenSleep(false);
    Bridge.setBrightness(state.settings.brightness);
    if (manual) {
      // Keep awake for at least 30 minutes if Dad taps screen during night schedule
      state.manualWakeUntil = Date.now() + 30 * 60 * 1000;
    }
    resetSlideTimer();
  }

  function checkSleepSchedule() {
    if (!state.settings.sleepEnabled) return;
    if (Date.now() < state.manualWakeUntil) return;

    const now = new Date();
    const cur = now.getHours() * 60 + now.getMinutes();
    const start = parseHHMM(state.settings.sleepStart);
    const end = parseHHMM(state.settings.sleepEnd);
    if (start == null || end == null || start === end) return;

    const inWindow = start < end ? cur >= start && cur < end : cur >= start || cur < end;
    if (inWindow && !state.sleeping) {
      enterSleep();
    } else if (!inWindow && state.sleeping) {
      exitSleep(false);
    }
  }

  function parseHHMM(s) {
    if (!s || !s.includes(":")) return null;
    const [h, m] = s.split(":").map(Number);
    if (Number.isNaN(h) || Number.isNaN(m)) return null;
    return h * 60 + m;
  }

  // Actions
  async function toggleFavorite() {
    const p = currentPhoto();
    if (!p) return;
    try {
      const updated = await apiJSON(`/api/photos/${p.id}/favorite`, { method: "POST", body: "{}" });
      state.photos[state.index] = updated;
      updateFavoriteButton(updated);
      if (updated.favorite) {
        const burst = $("#heart-burst");
        burst.classList.remove("pop");
        void burst.offsetWidth;
        burst.classList.add("pop");
        showToast("Added to Favorites ♥");
      } else {
        showToast("Removed from Favorites");
      }
    } catch (_) {}
  }

  async function hideCurrentPhoto() {
    const p = currentPhoto();
    if (!p) return;
    try {
      await apiJSON(`/api/photos/${p.id}/hide`, {
        method: "POST",
        body: JSON.stringify({ hidden: true }),
      });
      closeModal("info-drawer");
      showToast("Photo hidden from slideshow", "Undo", async () => {
        await apiJSON(`/api/photos/${p.id}/hide`, {
          method: "POST",
          body: JSON.stringify({ hidden: false }),
        });
        await loadSlideshow(false);
      });
      await loadSlideshow(false);
    } catch (_) {}
  }

  async function rotateCurrentPhoto() {
    const p = currentPhoto();
    if (!p) return;
    try {
      const updated = await apiJSON(`/api/photos/${p.id}/rotate`, { method: "POST" });
      state.cacheBust[p.id] = Date.now();
      state.photos[state.index] = updated;
      renderSlide(state.index, "fade");
      showToast("Rotated 90°");
    } catch (_) {
      showToast("Could not rotate photo");
    }
  }

  // SSE live updates
  function connectSSE() {
    const es = new EventSource("/api/events");
    es.addEventListener("photo_added", (e) => {
      try {
        const p = JSON.parse(e.data);
        if (p.source === "family") {
          showToast(p.sender ? `New photo from ${p.sender}!` : "New family photo arrived!");
        }
        loadSlideshow(state.photos.length === 0);
      } catch (_) {}
    });
    es.addEventListener("public_progress", (e) => {
      try {
        const ps = JSON.parse(e.data);
        updatePublicStatus(ps);
        if (!ps.refreshing && ps.lastError) {
          showToast(`Public sync: ${ps.lastError}`);
        }
      } catch (_) {}
    });
    es.addEventListener("photo_deleted", () => loadSlideshow(false));
    es.addEventListener("notice_changed", (e) => {
      try {
        const data = JSON.parse(e.data);
        state.settings.notice = data.notice || null;
        renderNotice();
      } catch (_) {}
    });
    es.addEventListener("settings_changed", (e) => {
      try {
        const updated = JSON.parse(e.data);
        const modeOrOrderChanged =
          updated.mode !== state.settings.mode ||
          updated.album !== state.settings.album ||
          updated.order !== state.settings.order ||
          updated.publicCategory !== state.settings.publicCategory ||
          updated.publicSource !== state.settings.publicSource;
        state.settings = { ...state.settings, ...updated };
        applySettingsVisuals();
        syncHUDControls();
        syncSettingsUI();
        if (modeOrOrderChanged) loadSlideshow(true);
      } catch (_) {}
    });
    es.onerror = () => {
      es.close();
      setTimeout(connectSSE, 5000);
    };
  }

  // Event bindings
  function bindUI() {
    // Mode switcher
    $$(".mode-btn").forEach((btn) => {
      btn.addEventListener("click", async () => {
        bumpHUDTimer();
        const mode = btn.dataset.mode;
        await patchSettings({ mode });
        await loadSlideshow(true);
      });
    });

    $("#album-select").addEventListener("change", async (e) => {
      bumpHUDTimer();
      await patchSettings({ album: e.target.value });
      await loadSlideshow(true);
    });

    $("#pub-cat-select").addEventListener("change", async (e) => {
      bumpHUDTimer();
      await patchSettings({ publicCategory: e.target.value });
      await loadSlideshow(true);
    });

    $("#btn-prev").addEventListener("click", () => { bumpHUDTimer(); prevSlide(true); });
    $("#btn-next").addEventListener("click", () => { bumpHUDTimer(); nextSlide(true); });
    $("#btn-play").addEventListener("click", () => { bumpHUDTimer(); setPaused(!state.paused); });
    $("#btn-fav").addEventListener("click", () => { bumpHUDTimer(); toggleFavorite(); });
    $("#btn-info").addEventListener("click", () => openModal("info-drawer"));
    $("#btn-settings").addEventListener("click", () => openModal("settings-modal"));
    $("#notice-dismiss").addEventListener("click", async (e) => {
      e.stopPropagation();
      state.settings.notice = null;
      renderNotice();
      try {
        await apiJSON("/api/notice", { method: "DELETE" });
      } catch (_) {}
    });

    $("#btn-fit").addEventListener("click", () => {
      bumpHUDTimer();
      const order = ["ambient", "fill", "matte"];
      const next = order[(order.indexOf(state.settings.fitMode) + 1) % order.length];
      patchSettings({ fitMode: next });
      const labels = { ambient: "Ambient Glow", fill: "Fill Screen", matte: "Dark Matte" };
      showToast(`Framing: ${labels[next]}`);
    });

    // Empty state actions
    $("#empty-public-btn").addEventListener("click", async () => {
      if (state.settings.mode === "public") {
        await triggerPublicRefresh();
      } else {
        await patchSettings({ mode: "public" });
        await loadSlideshow(true);
      }
    });
    $("#empty-settings-btn").addEventListener("click", () => openModal("settings-modal"));

    // Drawer / modal close triggers
    $$("[data-close]").forEach((el) => {
      el.addEventListener("click", () => closeModal(el.dataset.close));
    });

    // Info drawer quick actions
    $("#info-rotate").addEventListener("click", rotateCurrentPhoto);
    $("#info-fav").addEventListener("click", toggleFavorite);
    $("#info-hide").addEventListener("click", hideCurrentPhoto);

    // Settings tabs
    $$(".tab-btn").forEach((btn) => {
      btn.addEventListener("click", () => {
        const tab = btn.dataset.tab;
        $$(".tab-btn").forEach((b) => b.classList.toggle("active", b === btn));
        $$(".tab-pane").forEach((p) => p.classList.toggle("active", p.dataset.pane === tab));
      });
    });

    // Settings option chips
    $$(".chip-row").forEach((row) => {
      const key = row.dataset.setting;
      row.querySelectorAll(".opt-chip").forEach((btn) => {
        btn.addEventListener("click", async () => {
          const val = key === "intervalSec" ? Number(btn.dataset.val) : btn.dataset.val;
          await patchSettings({ [key]: val });
          if (key === "order" || key === "publicSource" || key === "publicCategory") {
            await loadSlideshow(true);
          }
        });
      });
    });

    // Toggles
    ["showClock", "clock24h", "showCaption", "showDateBadge", "sleepEnabled"].forEach((key) => {
      $(`#set-${key}`).addEventListener("change", (e) => {
        patchSettings({ [key]: e.target.checked });
      });
    });

    // Brightness slider
    let brightTimer = null;
    $("#set-brightness").addEventListener("input", (e) => {
      const v = Number(e.target.value);
      $("#brightness-pct").textContent = `${Math.round((v / 255) * 100)}%`;
      Bridge.setBrightness(v);
      clearTimeout(brightTimer);
      brightTimer = setTimeout(() => patchSettings({ brightness: v }), 220);
    });

    $("#set-sleepStart").addEventListener("change", (e) => patchSettings({ sleepStart: e.target.value }));
    $("#set-sleepEnd").addEventListener("change", (e) => patchSettings({ sleepEnd: e.target.value }));
    $("#btn-sleep-now").addEventListener("click", enterSleep);
    $("#sleep-overlay").addEventListener("click", () => exitSleep(true));

    // Public refresh & search
    $("#btn-refresh-public").addEventListener("click", triggerPublicRefresh);

    $("#pub-search-form").addEventListener("submit", async (e) => {
      e.preventDefault();
      const q = $("#pub-search-input").value.trim();
      if (!q) return;
      await triggerPublicSearch(q);
    });

    $$(".tag-chip").forEach((chip) => {
      chip.addEventListener("click", async () => {
        const q = chip.dataset.q;
        $("#pub-search-input").value = q;
        await triggerPublicSearch(q);
      });
    });

    // Device actions
    $("#btn-wifi").addEventListener("click", () => Bridge.openWifiSettings());
    $("#btn-tailscale").addEventListener("click", () => Bridge.launchApp("com.tailscale.ipn", "Tailscale"));
    $("#btn-reload").addEventListener("click", () => window.location.reload());
    $("#btn-reboot").addEventListener("click", () => {
      if (confirm("Reboot the photo frame now?")) {
        Bridge.rebootDevice();
      }
    });

    // Keyboard shortcuts (handy for dev testing + remote controls)
    window.addEventListener("keydown", (e) => {
      if (e.key === "ArrowRight") nextSlide(true);
      else if (e.key === "ArrowLeft") prevSlide(true);
      else if (e.key === " ") setPaused(!state.paused);
      else if (e.key === "i") openModal("info-drawer");
      else if (e.key === "Escape") {
        closeModal("info-drawer");
        closeModal("settings-modal");
        hideHUD();
      }
    });
  }

  async function triggerPublicRefresh() {
    try {
      const cat = state.settings.publicCategory || "all";
      const res = await apiJSON(`/api/public/refresh?category=${encodeURIComponent(cat)}`, { method: "POST" });
      updatePublicStatus(res.status);
      showToast("Downloading public gallery…");
    } catch (_) {
      showToast("Could not refresh public feed");
    }
  }

  async function triggerPublicSearch(query) {
    try {
      const res = await apiJSON("/api/public/search", {
        method: "POST",
        body: JSON.stringify({ query, switch: true }),
      });
      updatePublicStatus(res.status);
      showToast(`Searching "${query}"…`);
      await loadSlideshow(true);
    } catch (_) {
      showToast("Could not start search");
    }
  }

  // Touch & Pointer Gestures on Stage
  function bindGestures() {
    const stage = $("#stage");
    let startX = 0, startY = 0, startT = 0, tracking = false;

    stage.addEventListener("pointerdown", (e) => {
      if (e.target.closest(".hud-top, .hud-dock, .drawer, .modal, .empty-card, .sleep-overlay, .toast, .frame-notice")) {
        return;
      }
      tracking = true;
      startX = e.clientX;
      startY = e.clientY;
      startT = Date.now();
    });

    stage.addEventListener("pointerup", (e) => {
      if (!tracking) return;
      tracking = false;
      const dx = e.clientX - startX;
      const dy = e.clientY - startY;
      const dt = Date.now() - startT;

      if (Math.abs(dx) > 55 && Math.abs(dx) > Math.abs(dy) * 1.2) {
        if (dx < 0) nextSlide(true);
        else prevSlide(true);
        return;
      }
      if (dy < -65 && Math.abs(dy) > Math.abs(dx) * 1.2 && currentPhoto()) {
        openModal("info-drawer");
        return;
      }
      if (Math.abs(dx) < 18 && Math.abs(dy) < 18 && dt < 450) {
        if (state.hudOpen) hideHUD();
        else showHUD();
      }
    });
  }

  // Toast helper
  function showToast(msg, actionLabel = "", onAction = null) {
    const toast = $("#toast");
    const actBtn = $("#toast-action");
    $("#toast-msg").textContent = msg;
    if (actionLabel && onAction) {
      actBtn.textContent = actionLabel;
      actBtn.classList.remove("hidden");
      actBtn.onclick = () => {
        toast.classList.add("hidden");
        onAction();
      };
    } else {
      actBtn.classList.add("hidden");
      actBtn.onclick = null;
    }
    toast.classList.remove("hidden");
    clearTimeout(state.toastTimer);
    state.toastTimer = setTimeout(() => toast.classList.add("hidden"), actionLabel ? 6500 : 3200);
  }

  function formatShortDate(iso) {
    if (!iso) return "";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    return d.toLocaleDateString(undefined, { month: "short", year: "numeric" });
  }

  function formatFullDate(iso) {
    if (!iso) return "";
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    return d.toLocaleDateString(undefined, {
      year: "numeric",
      month: "long",
      day: "numeric",
      hour: "numeric",
      minute: "2-digit",
    });
  }

  function esc(str) {
    return String(str)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  init();
})();
