(() => {
  "use strict";

  const $ = (s) => document.querySelector(s);
  const $$ = (s) => Array.from(document.querySelectorAll(s));

  const state = {
    token: "",
    photos: [],
    albums: [],
    settings: null,
    filter: "all",
    selectedFiles: [],
    editingPhoto: null,
    cacheBust: {},
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
    $("#up-sender").value = localStorage.getItem("frmaeo_sender") || "";

    const urlToken = new URLSearchParams(window.location.search).get("token");
    const savedToken = localStorage.getItem("frmaeo_token");
    const candidate = urlToken || savedToken;

    if (candidate) {
      const ok = await tryToken(candidate);
      if (ok && urlToken) {
        // Strip token from URL bar after saving
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
      if (!res.ok) throw new Error("Invalid family token");
      const data = await res.json();
      localStorage.setItem("frmaeo_token", state.token);
      $("#frame-title").textContent = data.frameName || "Dad's Frame";
      $("#auth-gate").classList.add("hidden");
      $("#app").classList.remove("hidden");
      $("#auth-error").classList.add("hidden");
      await refreshAll();
      return true;
    } catch (err) {
      state.token = "";
      $("#auth-error").textContent = "That token didn't match. Check Settings → Add Photos on the frame.";
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

  async function refreshAll() {
    const data = await api("/api/manage/photos");
    state.photos = data.photos || [];
    state.albums = data.albums || [];
    state.settings = data.settings || {};

    $("#tab-count").textContent = state.photos.length;
    $("#album-suggestions").innerHTML = state.albums.map((a) => `<option value="${esc(a)}"></option>`).join("");
    renderGallery();
    renderSettingsForm();
  }

  function renderGallery() {
    const filtered = state.photos.filter((p) => {
      switch (state.filter) {
        case "family": return p.source === "family" && !p.hidden;
        case "public": return p.source === "public" && !p.hidden;
        case "favorites": return p.favorite && !p.hidden;
        case "hidden": return p.hidden;
        default: return !p.hidden;
      }
    });

    const grid = $("#gallery-grid");
    $("#gallery-empty").classList.toggle("hidden", filtered.length > 0);
    grid.innerHTML = filtered.map((p) => {
      const bust = state.cacheBust[p.id] ? `?v=${state.cacheBust[p.id]}` : "";
      const title = p.caption || p.publicMeta?.title || p.filename || "Photo";
      const sub = p.sender || p.publicMeta?.artist || (p.album ? p.album : p.source);
      return `
        <article class="photo-card" data-id="${esc(p.id)}">
          <img src="/api/photos/${esc(p.id)}/thumb${bust}" loading="lazy" alt="${esc(title)}" />
          ${p.favorite ? `<span class="badge-fav">♥</span>` : ""}
          ${p.hidden ? `<span class="badge-hidden">Hidden</span>` : ""}
          <div class="meta">
            <strong>${esc(title)}</strong>
            <span class="muted">${esc(sub || "")}</span>
          </div>
        </article>
      `;
    }).join("");

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
    $("#rem-frameName").value = st.frameName || "Dad's Frame";
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
    $("#edit-fav").textContent = p.favorite ? "♥ Favorited" : "♡ Favorite";
    $("#edit-hide").textContent = p.hidden ? "Unhide" : "Hide";

    const exifParts = [];
    if (p.exif?.cameraModel) exifParts.push(p.exif.cameraModel);
    if (p.exif?.focalLength) exifParts.push(p.exif.focalLength);
    if (p.exif?.aperture) exifParts.push(p.exif.aperture);
    if (p.exif?.shutter) exifParts.push(p.exif.shutter);
    if (p.exif?.iso) exifParts.push(`ISO ${p.exif.iso}`);
    if (p.exif?.locationName) exifParts.push(p.exif.locationName);
    if (p.width && p.height) exifParts.push(`${p.width}×${p.height}`);
    $("#edit-exif").textContent = exifParts.join(" · ") || "No EXIF metadata";

    $("#edit-modal").classList.remove("hidden");
  }

  function closeEditModal() {
    $("#edit-modal").classList.add("hidden");
    state.editingPhoto = null;
  }

  function handleSelectedFiles(fileList) {
    state.selectedFiles = Array.from(fileList).filter((f) => f.type.startsWith("image/"));
    const grid = $("#preview-grid");
    grid.innerHTML = "";
    state.selectedFiles.forEach((f) => {
      const div = document.createElement("div");
      div.className = "preview-item";
      const img = document.createElement("img");
      img.src = URL.createObjectURL(f);
      div.appendChild(img);
      grid.appendChild(div);
    });
    grid.classList.toggle("hidden", state.selectedFiles.length === 0);
    const btn = $("#btn-upload");
    btn.disabled = state.selectedFiles.length === 0;
    btn.textContent = state.selectedFiles.length > 1
      ? `Send ${state.selectedFiles.length} Photos to Frame`
      : "Send to Frame";
  }

  function bindEvents() {
    $("#auth-form").addEventListener("submit", async (e) => {
      e.preventDefault();
      await tryToken($("#token-input").value);
    });
    $("#btn-logout").addEventListener("click", logout);

    // Navigation tabs
    $$(".nav-tab").forEach((btn) => {
      btn.addEventListener("click", () => {
        const t = btn.dataset.tab;
        $$(".nav-tab").forEach((b) => b.classList.toggle("active", b === btn));
        $$(".pane").forEach((p) => p.classList.toggle("active", p.dataset.pane === t));
      });
    });

    // Dropzone
    const dz = $("#dropzone");
    const fi = $("#file-input");
    fi.addEventListener("change", () => handleSelectedFiles(fi.files));
    dz.addEventListener("dragover", (e) => { e.preventDefault(); dz.classList.add("dragover"); });
    dz.addEventListener("dragleave", () => dz.classList.remove("dragover"));
    dz.addEventListener("drop", (e) => {
      e.preventDefault();
      dz.classList.remove("dragover");
      handleSelectedFiles(e.dataTransfer.files);
    });

    // Upload submit with progress
    $("#upload-form").addEventListener("submit", (e) => {
      e.preventDefault();
      if (state.selectedFiles.length === 0) return;

      const sender = $("#up-sender").value.trim();
      if (sender) localStorage.setItem("frmaeo_sender", sender);

      const fd = new FormData();
      state.selectedFiles.forEach((f) => fd.append("files", f));
      fd.append("sender", sender);
      fd.append("album", $("#up-album").value.trim());
      fd.append("caption", $("#up-caption").value.trim());

      const wrap = $("#upload-progress-wrap");
      const bar = $("#upload-progress-bar");
      const btn = $("#btn-upload");
      wrap.classList.remove("hidden");
      bar.style.width = "0%";
      btn.disabled = true;
      btn.textContent = "Uploading...";

      const xhr = new XMLHttpRequest();
      xhr.open("POST", "/api/manage/upload");
      xhr.setRequestHeader("X-Frame-Token", state.token);
      xhr.upload.onprogress = (ev) => {
        if (ev.lengthComputable) {
          bar.style.width = `${Math.round((ev.loaded / ev.total) * 100)}%`;
        }
      };
      xhr.onload = async () => {
        wrap.classList.add("hidden");
        if (xhr.status >= 200 && xhr.status < 300) {
          const res = JSON.parse(xhr.responseText);
          showToast(`Sent ${(res.uploaded || []).length} photo(s) to Dad's frame!`);
          state.selectedFiles = [];
          fi.value = "";
          $("#up-caption").value = "";
          $("#preview-grid").innerHTML = "";
          $("#preview-grid").classList.add("hidden");
          btn.textContent = "Send to Frame";
          btn.disabled = true;
          await refreshAll();
        } else {
          showToast("Upload failed — check file format or size.");
          btn.disabled = false;
          btn.textContent = "Send to Frame";
        }
      };
      xhr.onerror = () => {
        wrap.classList.add("hidden");
        btn.disabled = false;
        showToast("Network error during upload.");
      };
      xhr.send(fd);
    });

    // Gallery filters
    $$(".filter-pill").forEach((pill) => {
      pill.addEventListener("click", () => {
        state.filter = pill.dataset.filter;
        $$(".filter-pill").forEach((p) => p.classList.toggle("active", p === pill));
        renderGallery();
      });
    });

    // Edit modal actions
    $("#edit-close").addEventListener("click", closeEditModal);
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
      showToast("Saved!");
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
      $("#edit-fav").textContent = updated.favorite ? "♥ Favorited" : "♡ Favorite";
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
      if (!confirm("Permanently delete this photo from the frame?")) return;
      await api(`/api/manage/photos/${p.id}`, { method: "DELETE" });
      showToast("Photo deleted");
      closeEditModal();
      await refreshAll();
    });

    // Remote settings save
    $("#btn-save-settings").addEventListener("click", async () => {
      await api("/api/settings", {
        method: "PATCH",
        body: JSON.stringify({
          mode: $("#rem-mode").value,
          intervalSec: Number($("#rem-interval").value),
          order: $("#rem-order").value,
          fitMode: $("#rem-fit").value,
          publicCategory: $("#rem-publicCategory").value,
          frameName: $("#rem-frameName").value.trim(),
          showClock: $("#rem-showClock").checked,
          showCaption: $("#rem-showCaption").checked,
          sleepEnabled: $("#rem-sleepEnabled").checked,
          sleepStart: $("#rem-sleepStart").value,
          sleepEnd: $("#rem-sleepEnd").value,
        }),
      });
      showToast("Frame settings updated!");
      await refreshAll();
    });

    $("#rem-search-form").addEventListener("submit", async (e) => {
      e.preventDefault();
      const q = $("#rem-search-input").value.trim();
      if (!q) return;
      const btn = $("#rem-search-btn");
      btn.disabled = true;
      btn.textContent = "Searching…";
      try {
        await api("/api/public/search", {
          method: "POST",
          body: JSON.stringify({ query: q, switch: true }),
        });
        showToast(`Searching "${q}" on Dad's frame!`);
        $("#rem-search-input").value = "";
      } catch (_) {
        showToast("Could not start public search.");
      } finally {
        btn.disabled = false;
        btn.textContent = "Search & Show";
      }
    });
  }

  let toastTimer = null;
  function showToast(msg) {
    const el = $("#toast");
    el.textContent = msg;
    el.classList.remove("hidden");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.classList.add("hidden"), 3000);
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
