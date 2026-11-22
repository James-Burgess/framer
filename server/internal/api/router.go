package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"frmaeo/internal/media"
	"frmaeo/internal/publicfeed"
	"frmaeo/internal/store"
)

const Version = "1.0.0"

type Server struct {
	store     *store.Store
	pubFeed   *publicfeed.Manager
	webFS     fs.FS
	startedAt time.Time
	port      int

	sseMu      sync.Mutex
	sseClients map[chan string]struct{}
}

func NewServer(st *store.Store, webFS fs.FS, port int) *Server {
	s := &Server{
		store:      st,
		webFS:      webFS,
		startedAt:  time.Now(),
		port:       port,
		sseClients: make(map[chan string]struct{}),
	}
	s.pubFeed = publicfeed.NewManager(
		st,
		func(p *store.Photo) {
			s.Broadcast("photo_added", p)
		},
		func(ps publicfeed.Status) {
			s.Broadcast("public_progress", ps)
		},
	)
	return s
}

func (s *Server) Broadcast(event string, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	msg := fmt.Sprintf("event: %s\ndata: %s\n\n", event, string(b))
	s.sseMu.Lock()
	defer s.sseMu.Unlock()
	for ch := range s.sseClients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health & SSE
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/events", s.handleSSE)

	// Slideshow & Kiosk interactions
	mux.HandleFunc("GET /api/slideshow", s.handleSlideshow)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PATCH /api/settings", s.handlePatchSettings)
	mux.HandleFunc("POST /api/photos/{id}/favorite", s.handleFavorite)
	mux.HandleFunc("POST /api/photos/{id}/hide", s.handleHide)
	mux.HandleFunc("POST /api/photos/{id}/rotate", s.handleRotate)
	mux.HandleFunc("GET /api/photos/{id}/display", s.handleServePhoto("display"))
	mux.HandleFunc("GET /api/photos/{id}/thumb", s.handleServePhoto("thumbs"))
	mux.HandleFunc("GET /api/photos/{id}/original", s.handleServeOriginal)

	// Public feed
	mux.HandleFunc("POST /api/public/refresh", s.handlePublicRefresh)
	mux.HandleFunc("POST /api/public/search", s.handlePublicSearch)
	mux.HandleFunc("GET /api/public/status", s.handlePublicStatus)

	// System stats & QR
	mux.HandleFunc("GET /api/system/stats", s.handleStats)
	mux.HandleFunc("GET /api/qr.svg", s.handleQR)

	// Token-gated Family Management API
	mux.HandleFunc("POST /api/auth/verify", s.handleAuthVerify)
	mux.HandleFunc("GET /api/manage/photos", s.requireToken(s.handleManageList))
	mux.HandleFunc("POST /api/manage/upload", s.requireToken(s.handleUpload))
	mux.HandleFunc("PATCH /api/manage/photos/{id}", s.requireToken(s.handleManageUpdate))
	mux.HandleFunc("DELETE /api/manage/photos/{id}", s.requireToken(s.handleManageDelete))

	// Static WebUIs (Kiosk at /, Manage at /manage)
	kioskSub, _ := fs.Sub(s.webFS, "web/kiosk")
	manageSub, _ := fs.Sub(s.webFS, "web/manage")
	kioskFS := http.FileServer(http.FS(kioskSub))
	manageFS := http.StripPrefix("/manage/", http.FileServer(http.FS(manageSub)))

	mux.HandleFunc("GET /manage", func(w http.ResponseWriter, r *http.Request) {
		target := "/manage/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	})
	mux.Handle("GET /manage/", manageFS)
	mux.Handle("GET /", kioskFS)

	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"version": Version,
	})
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 16)
	s.sseMu.Lock()
	s.sseClients[ch] = struct{}{}
	s.sseMu.Unlock()
	defer func() {
		s.sseMu.Lock()
		delete(s.sseClients, ch)
		s.sseMu.Unlock()
	}()

	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			_, _ = fmt.Fprint(w, msg)
			flusher.Flush()
		case <-ticker.C:
			_, _ = fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

func (s *Server) handleSlideshow(w http.ResponseWriter, r *http.Request) {
	settings := s.store.GetSettings()
	q := r.URL.Query()
	mode := q.Get("mode")
	if mode == "" {
		mode = settings.Mode
	}
	album := q.Get("album")
	if album == "" && q.Get("mode") == "" {
		album = settings.Album
	}
	pubCat := q.Get("publicCategory")
	if pubCat == "" {
		pubCat = q.Get("publicSource")
	}
	if pubCat == "" {
		pubCat = settings.PublicCategory
		if pubCat == "" || pubCat == "all" {
			pubCat = settings.PublicSource
		}
	}
	order := q.Get("order")
	if order == "" {
		order = settings.Order
	}

	photos := s.store.SlideshowPlaylist(mode, album, pubCat, order)

	// If switching into public/mix and we have 0 photos for this category, trigger background fetch
	if (mode == "public" || mode == "mix") && len(photos) == 0 {
		s.pubFeed.RefreshAsync(pubCat)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"photos":            photos,
		"albums":            s.store.Albums(),
		"publicCategories":  s.store.PublicCategories(),
		"builtinCategories": publicfeed.BuiltinCategories,
		"counts":            s.store.Counts(),
		"settings":          settings,
		"publicStatus":      s.pubFeed.Status(),
	})
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.GetSettings())
}

func (s *Server) handlePatchSettings(w http.ResponseWriter, r *http.Request) {
	// Require token if request is not from loopback
	if !isLoopback(r.RemoteAddr) && !s.validToken(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		return
	}

	var patch map[string]any
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	updated, err := s.store.UpdateSettings(func(st *store.Settings) {
		if v, ok := patch["mode"].(string); ok {
			st.Mode = v
		}
		if v, ok := patch["album"].(string); ok {
			st.Album = v
		}
		if v, ok := patch["intervalSec"].(float64); ok {
			st.IntervalSec = int(v)
		}
		if v, ok := patch["order"].(string); ok {
			st.Order = v
		}
		if v, ok := patch["transition"].(string); ok {
			st.Transition = v
		}
		if v, ok := patch["fitMode"].(string); ok {
			st.FitMode = v
		}
		if v, ok := patch["showClock"].(bool); ok {
			st.ShowClock = v
		}
		if v, ok := patch["clock24h"].(bool); ok {
			st.Clock24h = v
		}
		if v, ok := patch["showCaption"].(bool); ok {
			st.ShowCaption = v
		}
		if v, ok := patch["showDateBadge"].(bool); ok {
			st.ShowDateBadge = v
		}
		if v, ok := patch["publicSource"].(string); ok {
			st.PublicSource = v
		}
		if v, ok := patch["publicCategory"].(string); ok {
			st.PublicCategory = v
			st.PublicSource = v
		}
		if v, ok := patch["brightness"].(float64); ok {
			st.Brightness = int(v)
		}
		if v, ok := patch["sleepEnabled"].(bool); ok {
			st.SleepEnabled = v
		}
		if v, ok := patch["sleepStart"].(string); ok {
			st.SleepStart = v
		}
		if v, ok := patch["sleepEnd"].(string); ok {
			st.SleepEnd = v
		}
		if v, ok := patch["familyToken"].(string); ok && strings.TrimSpace(v) != "" {
			st.FamilyToken = strings.TrimSpace(v)
		}
		if v, ok := patch["frameName"].(string); ok && strings.TrimSpace(v) != "" {
			st.FrameName = strings.TrimSpace(v)
		}
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.Broadcast("settings_changed", updated)
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleFavorite(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Favorite *bool `json:"favorite"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)

	p, err := s.store.UpdatePhoto(id, func(ph *store.Photo) {
		if body.Favorite != nil {
			ph.Favorite = *body.Favorite
		} else {
			ph.Favorite = !ph.Favorite
		}
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "photo not found"})
		return
	}
	s.Broadcast("photo_updated", p)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleHide(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Hidden *bool `json:"hidden"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body)

	p, err := s.store.UpdatePhoto(id, func(ph *store.Photo) {
		if body.Hidden != nil {
			ph.Hidden = *body.Hidden
		} else {
			ph.Hidden = !ph.Hidden
		}
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "photo not found"})
		return
	}
	s.Broadcast("photo_updated", p)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleRotate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.GetPhoto(id); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "photo not found"})
		return
	}
	nw, nh, err := media.Rotate90CW(s.store, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	p, err := s.store.UpdatePhoto(id, func(ph *store.Photo) {
		ph.Rotation = (ph.Rotation + 90) % 360
		ph.Width = nw
		ph.Height = nh
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.Broadcast("photo_updated", p)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleServePhoto(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := filepath.Base(r.PathValue("id"))
		var dir string
		if kind == "thumbs" {
			dir = s.store.ThumbsDir()
		} else {
			dir = s.store.DisplayDir()
		}
		path := filepath.Join(dir, id+".jpg")
		info, err := os.Stat(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeContent(w, r, id+".jpg", info.ModTime(), bytes.NewReader(data))
	}
}

func (s *Server) handleServeOriginal(w http.ResponseWriter, r *http.Request) {
	id := filepath.Base(r.PathValue("id"))
	p, err := s.store.GetPhoto(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ext := strings.ToLower(filepath.Ext(p.Filename))
	if ext == "" {
		ext = ".jpg"
	}
	path := filepath.Join(s.store.OriginalsDir(), p.ID+ext)
	info, err := os.Stat(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, p.Filename, info.ModTime(), bytes.NewReader(data))
}

func (s *Server) handlePublicRefresh(w http.ResponseWriter, r *http.Request) {
	cat := r.URL.Query().Get("category")
	if cat == "" {
		cat = r.URL.Query().Get("source")
	}
	if cat == "" {
		st := s.store.GetSettings()
		cat = st.PublicCategory
		if cat == "" {
			cat = st.PublicSource
		}
	}
	started := s.pubFeed.RefreshAsync(cat)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"started": started,
		"status":  s.pubFeed.Status(),
	})
}

func (s *Server) handlePublicSearch(w http.ResponseWriter, r *http.Request) {
	if !isLoopback(r.RemoteAddr) && !s.validToken(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var body struct {
		Query  string `json:"query"`
		Switch bool   `json:"switch"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	q := strings.TrimSpace(body.Query)
	if q == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "query required"})
		return
	}
	catKey := "search:" + strings.ToLower(q)
	updated, _ := s.store.UpdateSettings(func(st *store.Settings) {
		st.PublicCategory = catKey
		st.PublicSource = catKey
		if body.Switch {
			st.Mode = "public"
		}
	})
	s.Broadcast("settings_changed", updated)
	started := s.pubFeed.SearchAsync(q)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"started":  started,
		"category": catKey,
		"status":   s.pubFeed.Status(),
	})
}

func (s *Server) handlePublicStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.pubFeed.Status())
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	var stat syscall.Statfs_t
	var totalBytes, freeBytes uint64
	if err := syscall.Statfs(s.store.DataDir(), &stat); err == nil {
		totalBytes = stat.Blocks * uint64(stat.Bsize)
		freeBytes = stat.Bavail * uint64(stat.Bsize)
	}
	lanIP, tailscaleIP := detectIPs()
	hostname, _ := os.Hostname()
	settings := s.store.GetSettings()

	manageHost := "frame.local"
	if lanIP != "" && hostname != "frame" {
		// Still advertise frame.local as friendly name, plus explicit LAN IP
		manageHost = lanIP
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"version":      Version,
		"uptimeSec":    int64(time.Since(s.startedAt).Seconds()),
		"hostname":     hostname,
		"port":         s.port,
		"lanIp":        lanIP,
		"tailscaleIp":  tailscaleIP,
		"manageUrl":    fmt.Sprintf("http://%s:%d/manage", manageHost, s.port),
		"friendlyUrl":  fmt.Sprintf("http://frame.local:%d/manage", s.port),
		"totalBytes":   totalBytes,
		"freeBytes":    freeBytes,
		"usedBytes":    totalBytes - freeBytes,
		"counts":       s.store.Counts(),
		"albums":       s.store.Albums(),
		"publicStatus": s.pubFeed.Status(),
		"frameName":    settings.FrameName,
	})
}

func (s *Server) handleQR(w http.ResponseWriter, r *http.Request) {
	settings := s.store.GetSettings()
	lanIP, _ := detectIPs()

	host := r.URL.Query().Get("host")
	if host == "" {
		if lanIP != "" {
			host = fmt.Sprintf("%s:%d", lanIP, s.port)
		} else {
			host = fmt.Sprintf("frame.local:%d", s.port)
		}
	}
	target := fmt.Sprintf("http://%s/manage?token=%s", host, url.QueryEscape(settings.FamilyToken))
	svg, err := media.GenerateQRSVG(target)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(svg))
}

func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	if !s.validToken(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "Invalid family token"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"frameName": s.store.GetSettings().FrameName,
	})
}

func (s *Server) handleManageList(w http.ResponseWriter, r *http.Request) {
	source := r.URL.Query().Get("source")
	writeJSON(w, http.StatusOK, map[string]any{
		"photos":   s.store.ListAll(source),
		"albums":   s.store.Albums(),
		"counts":   s.store.Counts(),
		"settings": s.store.GetSettings(),
	})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Up to 64 MB per batch upload
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload too large or invalid form"})
		return
	}

	caption := strings.TrimSpace(r.FormValue("caption"))
	sender := strings.TrimSpace(r.FormValue("sender"))
	album := strings.TrimSpace(r.FormValue("album"))

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		files = r.MultipartForm.File["file"]
	}
	if len(files) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no files provided"})
		return
	}

	uploaded := make([]*store.Photo, 0, len(files))
	var errs []string

	for _, fh := range files {
		f, err := fh.Open()
		if err != nil {
			errs = append(errs, fh.Filename+": "+err.Error())
			continue
		}
		raw, err := io.ReadAll(io.LimitReader(f, 32<<20))
		_ = f.Close()
		if err != nil {
			errs = append(errs, fh.Filename+": "+err.Error())
			continue
		}

		id := store.NewID()
		ext := strings.ToLower(filepath.Ext(fh.Filename))
		if ext == "" {
			ext = ".jpg"
		}
		res, err := media.IngestBytes(s.store, id, ext, raw)
		if err != nil {
			errs = append(errs, fh.Filename+": "+err.Error())
			continue
		}

		photo := &store.Photo{
			ID:         id,
			Filename:   fh.Filename,
			Source:     "family",
			Album:      album,
			Caption:    caption,
			Sender:     sender,
			Width:      res.Width,
			Height:     res.Height,
			SizeBytes:  res.SizeBytes,
			UploadedAt: time.Now().UTC(),
			Exif:       res.Exif,
		}
		if err := s.store.AddPhoto(photo); err != nil {
			media.RemoveFiles(s.store, photo)
			errs = append(errs, fh.Filename+": "+err.Error())
			continue
		}
		uploaded = append(uploaded, photo)
		s.Broadcast("photo_added", photo)
	}

	status := http.StatusCreated
	if len(uploaded) == 0 {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]any{
		"uploaded": uploaded,
		"errors":   errs,
	})
}

func (s *Server) handleManageUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Caption  *string `json:"caption"`
		Sender   *string `json:"sender"`
		Album    *string `json:"album"`
		Favorite *bool   `json:"favorite"`
		Hidden   *bool   `json:"hidden"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	p, err := s.store.UpdatePhoto(id, func(ph *store.Photo) {
		if body.Caption != nil {
			ph.Caption = strings.TrimSpace(*body.Caption)
		}
		if body.Sender != nil {
			ph.Sender = strings.TrimSpace(*body.Sender)
		}
		if body.Album != nil {
			ph.Album = strings.TrimSpace(*body.Album)
		}
		if body.Favorite != nil {
			ph.Favorite = *body.Favorite
		}
		if body.Hidden != nil {
			ph.Hidden = *body.Hidden
		}
	})
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "photo not found"})
		return
	}
	s.Broadcast("photo_updated", p)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleManageDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.store.DeletePhoto(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "photo not found"})
		return
	}
	media.RemoveFiles(s.store, p)
	s.Broadcast("photo_deleted", map[string]string{"id": id})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.validToken(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) validToken(r *http.Request) bool {
	expected := s.store.GetSettings().FamilyToken
	if expected == "" {
		return true
	}
	got := r.Header.Get("X-Frame-Token")
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	if got == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			got = strings.TrimPrefix(auth, "Bearer ")
		}
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(expected)) == 1
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func detectIPs() (lanIP, tailscaleIP string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil {
				continue
			}
			ip4 := ip.To4()
			// Tailscale uses CGNAT 100.64.0.0/10
			if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
				if tailscaleIP == "" {
					tailscaleIP = ip4.String()
				}
			} else if lanIP == "" {
				lanIP = ip4.String()
			}
		}
	}
	return lanIP, tailscaleIP
}

func ParsePort(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 8080
	}
	p, err := strconv.Atoi(portStr)
	if err != nil || p <= 0 {
		return 8080
	}
	return p
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
