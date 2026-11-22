(() => {
  "use strict";

  const $ = (s) => document.querySelector(s);
  const $$ = (s) => Array.from(document.querySelectorAll(s));

  const PAGE_META = {
    upload: {
      title: "Send Photos",
      subtitle: "Share moments directly to The Jimmy Frame over your local Wi-Fi.",
    },
    note: {
      title: "Frame Note",
      subtitle: "Display a live message banner across the top of the frame.",
    },
    gallery: {
      title: "Photo Library",
      subtitle: "Browse, edit captions, rotate, or organize photos on the frame.",
    },
    discover: {
      title: "Discover",
      subtitle: "Curated museum artwork, nature photography, and open-access image search.",
    },
    settings: {
      title: "Frame Settings",
      subtitle: "Slideshow pacing, overlays, and night sleep schedule. Changes apply immediately.",
    },
  };

  const state = {
    token: "",
    photos: [],
    albums: [],
    counts: { family: 0, public: 0, favorites: 0, hidden: 0 },
    settings: null,
    filter: "all",
    searchQuery: "",
    selectedFiles: [],
    editingPhoto: null,
    cacheBust: {},
    noteTone: "ocean",
    noteDurationMin: 0,
  };

  async function api(url, opts = {}) {
    const headers = { ...(opts.headers || {}) };
    if (state.token) headers["X-Frame-Token"] = state.token;
    if (opts.body && !(opts.body instanceof FormData) && !headers["Content-Type"]) {
      headers["Content-Type"] = "application/json";
    }
    const res = await fetch(url, { ...opts, headers });
    if (res.status === 401) {
      logout();
      throw new Error("Unauthorized");
    }
    if (!res.ok) {
      const txt = await res.text();
      throw new Error(txt || `HTTP ${res.status}`);
    }
    return res.json();
  }

  async function init() {
    bindEvents();
    const savedSender = localStorage.getItem("frmaeo_sender") || "";
    $("#up-sender").value = savedSender;
    $("#note-from").value = savedSender;
    updateNotePreview();

    const urlToken = new URLSearchParams(window.location.search).get("token");
    const savedToken = localStorage.getItem("frmaeo_token");
    const candidate = urlToken || savedToken;

    if (candidate) {
      const ok = await tryToken(candidate);
      if (ok && urlToken) {
        window.history.replaceState({}, "", "/manage/");
      }
    }
  }

  async function tryToken(tok) {
    state.token = tok.trim();
    try {
      const res = await fetch("/api/auth/verify", {
        method: "POST",
        headers: { "X-Frame-Token": state.token },
      });
      if (!res.ok) throw new Error("Invalid passcode");
      const data = await res.json();
      localStorage.setItem("frmaeo_token", state.token);
      applyFrameName(data.frameName || "The Jimmy Frame");
      $("#auth-gate").classList.add("hidden");
      $("#app").classList.remove("hidden");
      $("#auth-error").classList.add("hidden");
      await refreshAll();
      return true;
    } catch (_) {
      state.token = "";
      $("#auth-error").textContent = "That passcode didn't match. Check Settings → Add Photos on the frame.";
      $("#auth-error").classList.remove("hidden");
      return false;
    }
  }

  function logout() {
    state.token = "";
    localStorage.removeItem("frmaeo_token");
    $("#app").classList.add("hidden");
    $("#auth-gate").classList.remove("hidden");
  }

  function applyFrameName(name) {
    const label = (name || "The Jimmy Frame").trim();
    $("#frame-title").textContent = label;
    $("#auth-title").textContent = label;
    document.title = `${label} — Family Portal`;
  }

  function switchTab(tab) {
    $$(".nav-item").forEach((b) => b.classList.toggle("active", b.dataset.tab === tab));
    $$(".mnav-item").forEach((b) => b.classList.toggle("active", b.dataset.tab === tab));
    $$(".pane").forEach((p) => p.classList.toggle("active", p.dataset.pane === tab));
    const meta = PAGE_META[tab] || PAGE_META.upload;
    $("#page-title").textContent = meta.title;
    $("#page-subtitle").textContent = meta.subtitle;
  }

  async function refreshAll() {
    const data = await api("/api/manage/photos");
    state.photos = data.photos || [];
    state.albums = data.albums || [];
    state.counts = data.counts || { family: 0, public: 0, favorites: 0, hidden: 0 };
    state.settings = data.settings || {};

    applyFrameName(state.settings.frameName || "The Jimmy Frame");
    $("#tab-count").textContent = state.photos.filter((p) => !p.hidden).length;
    $("#header-stats-text").textContent = `${state.counts.family || 0} Family · ${state.counts.public || 0} Curated`;

    const modeNames = { family: "Family Slideshow", public: "Public Collections", mix: "Family + Public Mix" };
    $("#side-mode-label").textContent = modeNames[state.settings.mode] || "Frame Connected";

    $("#album-suggestions").innerHTML = state.albums.map((a) => `<option value="${esc(a)}"></option>`).join("");
    renderAlbumQuickChips();
    renderGallery();
    renderSettingsForm();
    syncNoticeState(state.settings.notice || null);
  }

  function renderAlbumQuickChips() {
    const wrap = $("#album-quick-chips");
    if (!wrap) return;
    if (state.albums.length === 0) {
      wrap.innerHTML = "";
      return;
    }
    wrap.innerHTML = state.albums
      .slice(0, 6)
      .map((a) => `<button type="button" class="chip-btn album-chip" data-album="${esc(a)}">${esc(a)}</button>`)
      .join("");
    wrap.querySelectorAll(".album-chip").forEach((btn) => {
      btn.addEventListener("click", () => {
        $("#up-album").value = btn.dataset.album;
      });
    });
  }

  function syncNoticeState(notice) {
    const active = !!(notice && notice.text && notice.text.trim());
    $("#nav-note-dot").classList.toggle("hidden", !active);
    $("#btn-clear-note").classList.toggle("hidden", !active);

    const badge = $("#note-status-badge");
    if (active) {
      badge.textContent = "Showing on frame";
      badge.className = "pill-badge active";
      $("#side-summary").textContent = `Active note: “${notice.text.trim()}”`;
      $("#side-note-cta").textContent = "Edit Frame Note";
      $("#note-input").value = notice.text.trim();
      if (notice.from) $("#note-from").value = notice.from;
      setNoteTone(notice.tone || "ocean");
    } else {
      badge.textContent = "No active note";
      badge.className = "pill-badge neutral";
      $("#side-summary").textContent = "Private local storage. Photos and notes appear live on the frame.";
      $("#side-note-cta").textContent = "Post a Frame Note";
    }
    updateNotePreview();
  }

  function setNoteTone(tone) {
    state.noteTone = ["ocean", "teal", "sun"].includes(tone) ? tone : "ocean";
    $$(".tone-opt").forEach((b) => b.classList.toggle("active", b.dataset.tone === state.noteTone));
    updateNotePreview();
  }

  function updateNotePreview() {
    const text = $("#note-input").value.trim();
    const from = $("#note-from").value.trim();
    $("#note-char-count").textContent = `${$("#note-input").value.length} / 160`;

    const banner = $("#note-preview-banner");
    banner.classList.remove("tone-ocean", "tone-teal", "tone-sun");
    banner.classList.add(`tone-${state.noteTone}`);
    $("#note-preview-text").textContent = text || "Your message will appear right here at the top of the screen";

    const fromEl = $("#note-preview-from");
    if (from) {
      fromEl.textContent = `From ${from}`;
      fromEl.classList.remove("hidden");
    } else {
      fromEl.classList.add("hidden");
    }
  }

  function renderGallery() {
    const q = state.searchQuery.toLowerCase();
    const filtered = state.photos.filter((p) => {
      let matchTab = true;
      switch (state.filter) {
        case "family": matchTab = p.source === "family" && !p.hidden; break;
        case "public": matchTab = p.source === "public" && !p.hidden; break;
        case "favorites": matchTab = p.favorite && !p.hidden; break;
        case "hidden": matchTab = p.hidden; break;
        default: matchTab = !p.hidden; break;
      }
      if (!matchTab) return false;
      if (!q) return true;
      const hay = [p.caption, p.sender, p.album, p.filename, p.publicMeta?.title, p.publicMeta?.artist]
        .filter(Boolean)
        .join(" ")
        .toLowerCase();
      return hay.includes(q);
    });

    const grid = $("#gallery-grid");
    $("#gallery-empty").classList.toggle("hidden", filtered.length > 0);
    grid.innerHTML = filtered
      .map((p) => {
        const bust = state.cacheBust[p.id] ? `?v=${state.cacheBust[p.id]}` : "";
        const title = p.caption || p.publicMeta?.title || p.filename || "Untitled";
        const sub = p.sender || p.publicMeta?.artist || p.album || (p.source === "public" ? "Public collection" : "Family photo");
        return `
          <article class="photo-card" data-id="${esc(p.id)}">
            <img src="/api/photos/${esc(p.id)}/thumb${bust}" loading="lazy" alt="${esc(title)}" />
            ${p.favorite ? `<span class="badge-fav" aria-label="Favorited"><svg viewBox="0 0 24 24" fill="currentColor"><path d="M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z"/></svg></span>` : ""}
            ${p.hidden ? `<span class="badge-hidden">Hidden</span>` : ""}
            <div class="meta">
              <strong>${esc(title)}</strong>
              <span>${esc(sub)}</span>
            </div>
          </article>
        `;
      })
      .join("");

    grid.querySelectorAll(".photo-card").forEach((card) => {
      card.addEventListener("click", () => {
        const p = state.photos.find((x) => x.id === card.dataset.id);
        if (p) openEditModal(p);
      });
    });
  }

  function renderSettingsForm() {
    const st = state.settings;
    if (!st) return;
    $("#rem-mode").value = st.mode || "family";
    $("#rem-interval").value = String(st.intervalSec || 20);
    $("#rem-order").value = st.order || "shuffle";
    $("#rem-fit").value = st.fitMode || "ambient";
    $("#rem-publicCategory").value = st.publicCategory || st.publicSource || "all";
    $("#rem-frameName").value = st.frameName || "The Jimmy Frame";
    $("#rem-showClock").checked = !!st.showClock;
    $("#rem-showCaption").checked = !!st.showCaption;
    $("#rem-sleepEnabled").checked = !!st.sleepEnabled;
    $("#rem-sleepStart").value = st.sleepStart || "22:00";
    $("#rem-sleepEnd").value = st.sleepEnd || "07:00";
  }

  function openEditModal(p) {
    state.editingPhoto = p;
    const bust = state.cacheBust[p.id] ? `?v=${state.cacheBust[p.id]}` : "";
    $("#edit-img").src = `/api/photos/${p.id}/display${bust}`;
    $("#edit-caption").value = p.caption || "";
    $("#edit-sender").value = p.sender || "";
    $("#edit-album").value = p.album || "";
    $("#edit-fav").textContent = p.favorite ? "Favorited" : "Favorite";
    $("#edit-hide").textContent = p.hidden ? "Unhide" : "Hide";

    const exifParts = [];
    if (p.exif?.cameraModel) exifParts.push(p.exif.cameraModel);
    if (p.exif?.focalLength) exifParts.push(p.exif.focalLength);
    if (p.exif?.aperture) exifParts.push(p.exif.aperture);
    if (p.exif?.shutter) exifParts.push(p.exif.shutter);
    if (p.exif?.iso) exifParts.push(`ISO ${p.exif.iso}`);
    if (p.exif?.locationName) exifParts.push(p.exif.locationName);
    if (p.width && p.height) exifParts.push(`${p.width} × ${p.height}`);
    $("#edit-exif").textContent = exifParts.join(" · ") || "No camera metadata recorded";

    $("#edit-modal").classList.remove("hidden");
  }

  function closeEditModal() {
    $("#edit-modal").classList.add("hidden");
    state.editingPhoto = null;
  }

  function isHeicFile(file) {
    const type = (file.type || "").toLowerCase();
    if (type === "image/heic" || type === "image/heif" || type === "image/heic-sequence" || type === "image/heif-sequence") {
      return true;
    }
    return /\.(heic|heif)$/i.test(file.name || "");
  }

  function isAcceptedImage(file) {
    if (!file) return false;
    if ((file.type || "").startsWith("image/")) return true;
    return /\.(heic|heif|jpe?g|png|webp|gif)$/i.test(file.name || "");
  }

  let heicLoaderPromise = null;
  function loadHeicConverter() {
    if (window.heic2any) return Promise.resolve(window.heic2any);
    if (heicLoaderPromise) return heicLoaderPromise;
    heicLoaderPromise = new Promise((resolve, reject) => {
      const s = document.createElement("script");
      s.src = "https://cdn.jsdelivr.net/npm/heic2any@0.0.4/dist/heic2any.min.js";
      s.async = true;
      s.onload = () => (window.heic2any ? resolve(window.heic2any) : reject(new Error("HEIC converter unavailable")));
      s.onerror = () => reject(new Error("Could not load HEIC converter (offline)"));
      document.head.appendChild(s);
    });
    return heicLoaderPromise;
  }

  async function decodeToBitmap(blob) {
    if (typeof createImageBitmap === "function") {
      try {
        return await createImageBitmap(blob, { imageOrientation: "from-image" });
      } catch (_) {
        try {
          return await createImageBitmap(blob);
        } catch (_) {}
      }
    }
    return new Promise((resolve, reject) => {
      const url = URL.createObjectURL(blob);
      const img = new Image();
      img.onload = () => { URL.revokeObjectURL(url); resolve(img); };
      img.onerror = () => { URL.revokeObjectURL(url); reject(new Error("decode failed")); };
      img.src = url;
    });
  }

  async function renderToJpegBlob(source, maxDim = 2048, quality = 0.9) {
    const sw = source.width || source.naturalWidth;
    const sh = source.height || source.naturalHeight;
    let tw = sw;
    let th = sh;
    if (sw > maxDim || sh > maxDim) {
      if (sw >= sh) {
        tw = maxDim;
        th = Math.max(1, Math.round((sh * maxDim) / sw));
      } else {
        th = maxDim;
        tw = Math.max(1, Math.round((sw * maxDim) / sh));
      }
    }
    const canvas = document.createElement("canvas");
    canvas.width = tw;
    canvas.height = th;
    const ctx = canvas.getContext("2d");
    ctx.drawImage(source, 0, 0, tw, th);
    if (typeof source.close === "function") source.close();

    return new Promise((resolve, reject) => {
      canvas.toBlob(
        (blob) => (blob ? resolve(blob) : reject(new Error("JPEG encoding failed"))),
        "image/jpeg",
        quality
      );
    });
  }

  async function preparePhotoForUpload(file, onStage) {
    const MAX_RAW_BYTES = 4 * 1024 * 1024; // 4 MB
    const MAX_RAW_DIM = 2400;
    const TARGET_DIM = 2048;
    const jpegName = (file.name || "photo.jpg").replace(/\.(heic|heif|png|webp|bmp|tiff?)$/i, ".jpg");
    const takenAt = file.lastModified ? new Date(file.lastModified).toISOString() : "";

    if (isHeicFile(file)) {
      if (onStage) onStage("Converting iPhone HEIC");
      let decoded = null;
      try {
        decoded = await decodeToBitmap(file);
      } catch (_) {
        const heic2any = await loadHeicConverter();
        const converted = await heic2any({ blob: file, toType: "image/jpeg", quality: 0.92 });
        const jpegBlob = Array.isArray(converted) ? converted[0] : converted;
        decoded = await decodeToBitmap(jpegBlob);
      }
      const outBlob = await renderToJpegBlob(decoded, TARGET_DIM, 0.9);
      return { blob: outBlob, filename: jpegName.endsWith(".jpg") ? jpegName : `${jpegName}.jpg`, takenAt };
    }

    // For standard images: keep untouched if already reasonable size & dimensions (preserves full EXIF + GPS!)
    try {
      const bmp = await decodeToBitmap(file);
      const sw = bmp.width || bmp.naturalWidth;
      const sh = bmp.height || bmp.naturalHeight;
      if (file.size <= MAX_RAW_BYTES && sw <= MAX_RAW_DIM && sh <= MAX_RAW_DIM) {
        if (typeof bmp.close === "function") bmp.close();
        return { blob: file, filename: file.name || "photo.jpg", takenAt };
      }
      if (onStage) onStage("Optimizing high-res photo");
      const outBlob = await renderToJpegBlob(bmp, TARGET_DIM, 0.9);
      return { blob: outBlob, filename: jpegName, takenAt };
    } catch (_) {
      // Fallback: let server attempt direct decode
      return { blob: file, filename: file.name || "photo.jpg", takenAt };
    }
  }

  function uploadSingleFile(prepared, meta, onProgress) {
    return new Promise((resolve, reject) => {
      const fd = new FormData();
      fd.append("files", prepared.blob, prepared.filename);
      fd.append("sender", meta.sender);
      fd.append("album", meta.album);
      fd.append("caption", meta.caption);
      if (prepared.takenAt) fd.append("takenAt", prepared.takenAt);

      const xhr = new XMLHttpRequest();
      xhr.open("POST", "/api/manage/upload");
      xhr.setRequestHeader("X-Frame-Token", state.token);
      xhr.upload.onprogress = (ev) => {
        if (ev.lengthComputable && onProgress) {
          onProgress(ev.loaded / ev.total);
        }
      };
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          try {
            resolve(JSON.parse(xhr.responseText));
          } catch (_) {
            resolve({ uploaded: [{}] });
          }
        } else if (xhr.status === 401) {
          logout();
          reject(new Error("Session locked"));
        } else {
          let msg = `HTTP ${xhr.status}`;
          try {
            const j = JSON.parse(xhr.responseText);
            if (j.error) msg = j.error;
            else if (j.errors && j.errors.length) msg = j.errors[0];
          } catch (_) {}
          reject(new Error(msg));
        }
      };
      xhr.onerror = () => reject(new Error("Network error"));
      xhr.send(fd);
    });
  }

  let nextQueueId = 1;
  let activePrepCount = 0;
  const MAX_CONCURRENT_PREP = 2;

  function handleSelectedFiles(fileList) {
    const incoming = Array.from(fileList).filter(isAcceptedImage);
    if (incoming.length === 0) return;

    incoming.forEach((file) => {
      const entry = {
        id: nextQueueId++,
        file,
        status: "queued", // "queued" | "preparing" | "ready" | "error"
        stage: isHeicFile(file) ? "Converting" : "Optimizing",
        previewUrl: "",
        prepared: null,
        error: "",
      };
      // Show immediate preview for non-HEIC files while optimizing in background
      if (!isHeicFile(file)) {
        entry.previewUrl = URL.createObjectURL(file);
      }
      state.selectedFiles.push(entry);
    });

    renderSelectedFiles();
    pumpPrepQueue();
  }

  function pumpPrepQueue() {
    while (activePrepCount < MAX_CONCURRENT_PREP) {
      const next = state.selectedFiles.find((item) => item.status === "queued");
      if (!next) break;
      next.status = "preparing";
      activePrepCount++;
      renderSelectedFiles();

      preparePhotoForUpload(next.file, (stage) => {
        next.stage = stage.includes("HEIC") ? "Converting" : "Optimizing";
      })
        .then((prepared) => {
          // Only update if user hasn't removed it from the queue
          if (!state.selectedFiles.includes(next)) return;
          if (next.previewUrl) URL.revokeObjectURL(next.previewUrl);
          next.prepared = prepared;
          next.previewUrl = URL.createObjectURL(prepared.blob);
          next.status = "ready";
        })
        .catch((err) => {
          if (!state.selectedFiles.includes(next)) return;
          next.status = "error";
          next.error = err?.message || "Unsupported";
        })
        .finally(() => {
          activePrepCount--;
          renderSelectedFiles();
          pumpPrepQueue();
        });
    }
  }

  function clearSelectedFiles() {
    state.selectedFiles.forEach((item) => {
      if (item.previewUrl) URL.revokeObjectURL(item.previewUrl);
    });
    state.selectedFiles = [];
    renderSelectedFiles();
  }

  function renderSelectedFiles() {
    const wrap = $("#preview-wrap");
    const grid = $("#preview-grid");
    grid.innerHTML = "";

    state.selectedFiles.forEach((item, idx) => {
      const div = document.createElement("div");
      div.className = "preview-item";

      if (item.status === "error") {
        const fb = document.createElement("div");
        fb.className = "preview-heic-fallback preview-error";
        fb.title = item.error;
        fb.innerHTML = `<strong>Error</strong><span>${esc(item.file.name)}</span>`;
        div.appendChild(fb);
      } else if (item.previewUrl) {
        const img = document.createElement("img");
        img.src = item.previewUrl;
        img.alt = item.file.name;
        div.appendChild(img);
      } else {
        const fb = document.createElement("div");
        fb.className = "preview-heic-fallback";
        fb.innerHTML = `<div class="preview-spinner" aria-hidden="true"></div><span>${esc(item.stage || "Preparing")}…</span>`;
        div.appendChild(fb);
      }

      const rm = document.createElement("button");
      rm.type = "button";
      rm.className = "preview-remove";
      rm.setAttribute("aria-label", `Remove ${item.file.name}`);
      rm.textContent = "×";
      rm.addEventListener("click", () => {
        if (item.previewUrl) URL.revokeObjectURL(item.previewUrl);
        state.selectedFiles.splice(idx, 1);
        renderSelectedFiles();
        pumpPrepQueue();
      });
      div.appendChild(rm);
      grid.appendChild(div);
    });

    const total = state.selectedFiles.length;
    const readyCount = state.selectedFiles.filter((i) => i.status === "ready").length;
    const pendingCount = state.selectedFiles.filter((i) => i.status === "queued" || i.status === "preparing").length;

    wrap.classList.toggle("hidden", total === 0);
    $("#preview-count").textContent = pendingCount > 0
      ? `Preparing ${readyCount} of ${total} ${total === 1 ? "photo" : "photos"}…`
      : `${total} ${total === 1 ? "photo" : "photos"} ready`;

    const btn = $("#btn-upload");
    if (total === 0) {
      btn.disabled = true;
      btn.textContent = "Send to Frame";
    } else if (pendingCount > 0) {
      btn.disabled = true;
      btn.textContent = `Optimizing (${readyCount}/${total})…`;
    } else if (readyCount === 0) {
      btn.disabled = true;
      btn.textContent = "No valid photos";
    } else {
      btn.disabled = false;
      btn.textContent = readyCount > 1 ? `Send ${readyCount} Photos to Frame` : "Send to Frame";
    }
  }

  function bindEvents() {
    $("#auth-form").addEventListener("submit", async (e) => {
      e.preventDefault();
      await tryToken($("#token-input").value);
    });
    $("#btn-logout").addEventListener("click", logout);

    // Navigation (sidebar + mobile bottom bar + quick CTAs)
    $$(".nav-item, .mnav-item").forEach((btn) => {
      btn.addEventListener("click", () => switchTab(btn.dataset.tab));
    });
    $("#side-quick-note").addEventListener("click", () => switchTab("note"));
    $("#empty-goto-upload").addEventListener("click", () => switchTab("upload"));

    // Dropzone & file selection
    const dz = $("#dropzone");
    const fi = $("#file-input");
    fi.addEventListener("change", () => {
      handleSelectedFiles(fi.files);
      fi.value = "";
    });
    dz.addEventListener("dragover", (e) => { e.preventDefault(); dz.classList.add("dragover"); });
    dz.addEventListener("dragleave", () => dz.classList.remove("dragover"));
    dz.addEventListener("drop", (e) => {
      e.preventDefault();
      dz.classList.remove("dragover");
      handleSelectedFiles(e.dataTransfer.files);
    });
    $("#btn-clear-files").addEventListener("click", clearSelectedFiles);

    // Upload submit: uses already-converted/optimized items for instant sending
    $("#upload-form").addEventListener("submit", async (e) => {
      e.preventDefault();
      const readyItems = state.selectedFiles.filter((item) => item.status === "ready" && item.prepared);
      if (readyItems.length === 0) return;

      const sender = $("#up-sender").value.trim();
      if (sender) localStorage.setItem("frmaeo_sender", sender);
      const meta = {
        sender,
        album: $("#up-album").value.trim(),
        caption: $("#up-caption").value.trim(),
      };

      const wrap = $("#upload-progress-wrap");
      const bar = $("#upload-progress-bar");
      const labelEl = $("#upload-progress-label");
      const pctEl = $("#upload-progress-pct");
      const btn = $("#btn-upload");

      const total = readyItems.length;
      let uploadedCount = 0;
      const failed = [];

      wrap.classList.remove("hidden");
      bar.style.width = "0%";
      pctEl.textContent = "0%";
      btn.disabled = true;
      btn.textContent = "Sending…";

      for (let i = 0; i < total; i++) {
        const item = readyItems[i];
        const basePct = (i / total) * 100;
        const slicePct = 100 / total;
        try {
          labelEl.textContent = `Sending ${i + 1} of ${total}…`;
          await uploadSingleFile(item.prepared, meta, (frac) => {
            const pct = Math.min(100, Math.round(basePct + frac * slicePct));
            bar.style.width = `${pct}%`;
            pctEl.textContent = `${pct}%`;
          });

          uploadedCount++;
          const donePct = Math.round(((i + 1) / total) * 100);
          bar.style.width = `${donePct}%`;
          pctEl.textContent = `${donePct}%`;
        } catch (err) {
          failed.push(`${item.file.name}: ${err.message || "failed"}`);
        }
      }

      wrap.classList.add("hidden");
      if (uploadedCount > 0 && failed.length === 0) {
        showToast(`Sent ${uploadedCount} ${uploadedCount === 1 ? "photo" : "photos"} to The Jimmy Frame`);
        $("#up-caption").value = "";
        clearSelectedFiles();
        await refreshAll();
      } else if (uploadedCount > 0) {
        showToast(`Sent ${uploadedCount} of ${total} photos (${failed.length} skipped: ${failed[0]})`);
        clearSelectedFiles();
        await refreshAll();
      } else {
        showToast(`Upload failed: ${failed[0] || "Could not upload photo"}`);
        renderSelectedFiles();
      }
    });

    // Frame Note composer
    $("#note-input").addEventListener("input", updateNotePreview);
    $("#note-from").addEventListener("input", updateNotePreview);
    $$(".note-preset").forEach((btn) => {
      btn.addEventListener("click", () => {
        $("#note-input").value = btn.dataset.text;
        updateNotePreview();
      });
    });
    $$(".tone-opt").forEach((btn) => {
      btn.addEventListener("click", () => setNoteTone(btn.dataset.tone));
    });
    $$("#note-duration-group .seg-btn").forEach((btn) => {
      btn.addEventListener("click", () => {
        state.noteDurationMin = Number(btn.dataset.min) || 0;
        $$("#note-duration-group .seg-btn").forEach((b) => b.classList.toggle("active", b === btn));
      });
    });

    $("#note-form").addEventListener("submit", async (e) => {
      e.preventDefault();
      const text = $("#note-input").value.trim();
      if (!text) {
        showToast("Write a short message first.");
        $("#note-input").focus();
        return;
      }
      const from = $("#note-from").value.trim();
      if (from) localStorage.setItem("frmaeo_sender", from);

      const btn = $("#btn-send-note");
      btn.disabled = true;
      try {
        const res = await api("/api/notice", {
          method: "POST",
          body: JSON.stringify({
            text,
            from,
            tone: state.noteTone,
            durationMin: state.noteDurationMin,
          }),
        });
        syncNoticeState(res.notice || null);
        showToast("Message is now displayed on The Jimmy Frame");
      } catch (_) {
        showToast("Could not send message to frame.");
      } finally {
        btn.disabled = false;
      }
    });

    $("#btn-clear-note").addEventListener("click", async () => {
      try {
        await api("/api/notice", { method: "DELETE" });
        $("#note-input").value = "";
        syncNoticeState(null);
        showToast("Frame note cleared");
      } catch (_) {
        showToast("Could not clear note.");
      }
    });

    // Gallery filters & search
    $$(".filter-pill").forEach((pill) => {
      pill.addEventListener("click", () => {
        state.filter = pill.dataset.filter;
        $$(".filter-pill").forEach((p) => p.classList.toggle("active", p === pill));
        renderGallery();
      });
    });
    $("#gallery-search").addEventListener("input", (e) => {
      state.searchQuery = e.target.value.trim();
      renderGallery();
    });

    // Edit modal actions
    $("#edit-close").addEventListener("click", closeEditModal);
    $("#edit-cancel").addEventListener("click", closeEditModal);
    $("#edit-backdrop").addEventListener("click", closeEditModal);

    $("#edit-save").addEventListener("click", async () => {
      const p = state.editingPhoto;
      if (!p) return;
      await api(`/api/manage/photos/${p.id}`, {
        method: "PATCH",
        body: JSON.stringify({
          caption: $("#edit-caption").value,
          sender: $("#edit-sender").value,
          album: $("#edit-album").value,
        }),
      });
      showToast("Photo details saved");
      closeEditModal();
      await refreshAll();
    });

    $("#edit-rotate").addEventListener("click", async () => {
      const p = state.editingPhoto;
      if (!p) return;
      await api(`/api/photos/${p.id}/rotate`, { method: "POST" });
      state.cacheBust[p.id] = Date.now();
      $("#edit-img").src = `/api/photos/${p.id}/display?v=${state.cacheBust[p.id]}`;
      showToast("Rotated 90°");
      await refreshAll();
    });

    $("#edit-fav").addEventListener("click", async () => {
      const p = state.editingPhoto;
      if (!p) return;
      const updated = await api(`/api/photos/${p.id}/favorite`, { method: "POST", body: "{}" });
      state.editingPhoto = updated;
      $("#edit-fav").textContent = updated.favorite ? "Favorited" : "Favorite";
      showToast(updated.favorite ? "Added to Favorites" : "Removed from Favorites");
      await refreshAll();
    });

    $("#edit-hide").addEventListener("click", async () => {
      const p = state.editingPhoto;
      if (!p) return;
      const updated = await api(`/api/photos/${p.id}/hide`, { method: "POST", body: "{}" });
      state.editingPhoto = updated;
      $("#edit-hide").textContent = updated.hidden ? "Unhide" : "Hide";
      showToast(updated.hidden ? "Hidden from slideshow" : "Visible in slideshow");
      await refreshAll();
    });

    $("#edit-delete").addEventListener("click", async () => {
      const p = state.editingPhoto;
      if (!p) return;
      if (!confirm("Permanently remove this photo from The Jimmy Frame?")) return;
      await api(`/api/manage/photos/${p.id}`, { method: "DELETE" });
      showToast("Photo removed");
      closeEditModal();
      await refreshAll();
    });

    // Discover: Search & Collection Cards
    $("#rem-search-form").addEventListener("submit", async (e) => {
      e.preventDefault();
      await runDiscoverSearch($("#rem-search-input").value.trim());
    });

    $$(".discover-tag").forEach((btn) => {
      btn.addEventListener("click", async () => {
        $("#rem-search-input").value = btn.dataset.q;
        await runDiscoverSearch(btn.dataset.q);
      });
    });

    $$(".collection-card").forEach((card) => {
      card.querySelector(".collection-go").addEventListener("click", async () => {
        const cat = card.dataset.cat;
        const title = card.querySelector("h3").textContent;
        try {
          await api("/api/settings", {
            method: "PATCH",
            body: JSON.stringify({ mode: "public", publicCategory: cat }),
          });
          await api(`/api/public/refresh?category=${encodeURIComponent(cat)}`, { method: "POST" });
          showToast(`Showing “${title}” on The Jimmy Frame`);
          await refreshAll();
        } catch (_) {
          showToast("Could not switch collection.");
        }
      });
    });

    // Remote Settings save
    $("#btn-save-settings").addEventListener("click", async () => {
      await api("/api/settings", {
        method: "PATCH",
        body: JSON.stringify({
          mode: $("#rem-mode").value,
          intervalSec: Number($("#rem-interval").value),
          order: $("#rem-order").value,
          fitMode: $("#rem-fit").value,
          publicCategory: $("#rem-publicCategory").value,
          frameName: $("#rem-frameName").value.trim() || "The Jimmy Frame",
          showClock: $("#rem-showClock").checked,
          showCaption: $("#rem-showCaption").checked,
          sleepEnabled: $("#rem-sleepEnabled").checked,
          sleepStart: $("#rem-sleepStart").value,
          sleepEnd: $("#rem-sleepEnd").value,
        }),
      });
      showToast("Settings saved to The Jimmy Frame");
      await refreshAll();
    });
  }

  async function runDiscoverSearch(q) {
    if (!q) return;
    const btn = $("#rem-search-btn");
    btn.disabled = true;
    btn.textContent = "Searching…";
    try {
      await api("/api/public/search", {
        method: "POST",
        body: JSON.stringify({ query: q, switch: true }),
      });
      showToast(`Searching “${q}” for The Jimmy Frame`);
      $("#rem-search-input").value = "";
      await refreshAll();
    } catch (_) {
      showToast("Could not start image search.");
    } finally {
      btn.disabled = false;
      btn.textContent = "Search & Display";
    }
  }

  let toastTimer = null;
  function showToast(msg) {
    const el = $("#toast");
    el.textContent = msg;
    el.classList.remove("hidden");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.classList.add("hidden"), 3200);
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
