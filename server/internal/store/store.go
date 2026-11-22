package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found")

type ExifData struct {
	TakenAt      string  `json:"takenAt,omitempty"`
	CameraMake   string  `json:"cameraMake,omitempty"`
	CameraModel  string  `json:"cameraModel,omitempty"`
	Lens         string  `json:"lens,omitempty"`
	FocalLength  string  `json:"focalLength,omitempty"`
	Aperture     string  `json:"aperture,omitempty"`
	Shutter      string  `json:"shutter,omitempty"`
	ISO          int     `json:"iso,omitempty"`
	Orientation  int     `json:"orientation,omitempty"`
	Lat          float64 `json:"lat,omitempty"`
	Lon          float64 `json:"lon,omitempty"`
	HasGPS       bool    `json:"hasGps,omitempty"`
	LocationName string  `json:"locationName,omitempty"`
}

type PublicMeta struct {
	Provider    string `json:"provider,omitempty"`    // "met" | "wikimedia" | "nasa"
	Category    string `json:"category,omitempty"`    // "fine_art" | "nature" | "space" | "architecture" | "vintage" | "search:<q>"
	SearchQuery string `json:"searchQuery,omitempty"`
	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	Date        string `json:"date,omitempty"`
	Description string `json:"description,omitempty"`
	SourceURL   string `json:"sourceUrl,omitempty"`
	ExternalID  string `json:"externalId,omitempty"`
}

type Photo struct {
	ID         string      `json:"id"`
	Filename   string      `json:"filename"`
	Source     string      `json:"source"` // "family" | "public"
	Album      string      `json:"album,omitempty"`
	Caption    string      `json:"caption,omitempty"`
	Sender     string      `json:"sender,omitempty"`
	Favorite   bool        `json:"favorite"`
	Hidden     bool        `json:"hidden"`
	Rotation   int         `json:"rotation"` // 0, 90, 180, 270
	Width      int         `json:"width"`
	Height     int         `json:"height"`
	SizeBytes  int64       `json:"sizeBytes"`
	UploadedAt time.Time   `json:"uploadedAt"`
	Exif       *ExifData   `json:"exif,omitempty"`
	PublicMeta *PublicMeta `json:"publicMeta,omitempty"`
}

type Notice struct {
	Text      string     `json:"text"`
	From      string     `json:"from,omitempty"`
	Tone      string     `json:"tone,omitempty"` // "ocean" | "teal" | "sun"
	UpdatedAt time.Time  `json:"updatedAt,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func (n *Notice) IsActive(now time.Time) bool {
	if n == nil || strings.TrimSpace(n.Text) == "" {
		return false
	}
	if n.ExpiresAt != nil && now.After(*n.ExpiresAt) {
		return false
	}
	return true
}

type Settings struct {
	Mode           string  `json:"mode"`          // "family" | "public" | "mix"
	Album          string  `json:"album"`         // "" (all) | "favorites" | <album name>
	IntervalSec    int     `json:"intervalSec"`   // 10, 20, 30, 60, 300, 900
	Order          string  `json:"order"`         // "shuffle" | "newest" | "chronological"
	Transition     string  `json:"transition"`    // "fade" | "kenburns" | "slide"
	FitMode        string  `json:"fitMode"`       // "ambient" | "fill" | "matte"
	ShowClock      bool    `json:"showClock"`
	Clock24h       bool    `json:"clock24h"`
	ShowCaption    bool    `json:"showCaption"`
	ShowDateBadge  bool    `json:"showDateBadge"`
	PublicSource   string  `json:"publicSource"`   // legacy/provider filter or category ("all" | "fine_art" | "nature" | "space" | "architecture" | "vintage" | "search:<q>")
	PublicCategory string  `json:"publicCategory"` // active public category or "search:<q>"
	Brightness     int     `json:"brightness"`     // 0..255
	SleepEnabled   bool    `json:"sleepEnabled"`
	SleepStart     string  `json:"sleepStart"` // "HH:MM"
	SleepEnd       string  `json:"sleepEnd"`   // "HH:MM"
	FamilyToken    string  `json:"familyToken"`
	FrameName      string  `json:"frameName"`
	Notice         *Notice `json:"notice,omitempty"`
}

type Library struct {
	Photos []*Photo `json:"photos"`
}

type Store struct {
	mu       sync.RWMutex
	dataDir  string
	photos   []*Photo
	byID     map[string]*Photo
	settings Settings
}

func DefaultSettings(token string) Settings {
	if token == "" {
		token = generateFriendlyToken()
	}
	return Settings{
		Mode:          "family",
		Album:         "",
		IntervalSec:   20,
		Order:         "shuffle",
		Transition:    "kenburns",
		FitMode:       "ambient",
		ShowClock:     true,
		Clock24h:      false,
		ShowCaption:   true,
		ShowDateBadge: true,
		PublicSource:   "all",
		PublicCategory: "all",
		Brightness:    200,
		SleepEnabled:  false,
		SleepStart:    "22:00",
		SleepEnd:      "07:00",
		FamilyToken:   token,
		FrameName:     "The Jimmy Frame",
	}
}

func generateFriendlyToken() string {
	words := []string{"sunny", "amber", "cedar", "maple", "river", "olive", "coral", "cloud"}
	wIdx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(words))))
	num, _ := rand.Int(rand.Reader, big.NewInt(9000))
	return fmt.Sprintf("%s-%d", words[wIdx.Int64()], 1000+num.Int64())
}

func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func Open(dataDir string, tokenOverride string) (*Store, error) {
	dirs := []string{
		dataDir,
		filepath.Join(dataDir, "originals"),
		filepath.Join(dataDir, "display"),
		filepath.Join(dataDir, "thumbs"),
		filepath.Join(dataDir, "public_cache"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	s := &Store{
		dataDir: dataDir,
		byID:    make(map[string]*Photo),
	}

	// Load or initialize settings
	settingsPath := filepath.Join(dataDir, "settings.json")
	if b, err := os.ReadFile(settingsPath); err == nil {
		s.settings = DefaultSettings("")
		if err := json.Unmarshal(b, &s.settings); err != nil {
			s.settings = DefaultSettings(tokenOverride)
		}
		if s.settings.FamilyToken == "" {
			s.settings.FamilyToken = generateFriendlyToken()
		}
		if s.settings.FrameName == "" || s.settings.FrameName == "Dad's Frame" {
			s.settings.FrameName = "The Jimmy Frame"
		}
	} else {
		s.settings = DefaultSettings(tokenOverride)
	}
	if tokenOverride != "" {
		s.settings.FamilyToken = tokenOverride
	}
	if err := s.saveSettingsLocked(); err != nil {
		return nil, err
	}

	// Load library
	libPath := filepath.Join(dataDir, "library.json")
	if b, err := os.ReadFile(libPath); err == nil {
		var lib Library
		if err := json.Unmarshal(b, &lib); err == nil {
			s.photos = lib.Photos
			for _, p := range s.photos {
				s.byID[p.ID] = p
			}
		}
	}

	return s, nil
}

func (s *Store) DataDir() string {
	return s.dataDir
}

func (s *Store) OriginalsDir() string {
	return filepath.Join(s.dataDir, "originals")
}

func (s *Store) DisplayDir() string {
	return filepath.Join(s.dataDir, "display")
}

func (s *Store) ThumbsDir() string {
	return filepath.Join(s.dataDir, "thumbs")
}

func (s *Store) PublicCacheDir() string {
	return filepath.Join(s.dataDir, "public_cache")
}

func (s *Store) GetSettings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st := s.settings
	if st.Notice != nil && !st.Notice.IsActive(time.Now()) {
		st.Notice = nil
	}
	return st
}

func (s *Store) SetNotice(n *Notice) (Settings, error) {
	return s.UpdateSettings(func(st *Settings) {
		if n == nil || strings.TrimSpace(n.Text) == "" {
			st.Notice = nil
			return
		}
		cp := *n
		cp.Text = strings.TrimSpace(cp.Text)
		cp.From = strings.TrimSpace(cp.From)
		switch cp.Tone {
		case "ocean", "teal", "sun":
		default:
			cp.Tone = "ocean"
		}
		if cp.UpdatedAt.IsZero() {
			cp.UpdatedAt = time.Now().UTC()
		}
		st.Notice = &cp
	})
}

func (s *Store) UpdateSettings(fn func(*Settings)) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.settings)
	s.sanitizeSettingsLocked()
	if err := s.saveSettingsLocked(); err != nil {
		return s.settings, err
	}
	return s.settings, nil
}

func (s *Store) sanitizeSettingsLocked() {
	switch s.settings.Mode {
	case "family", "public", "mix":
	default:
		s.settings.Mode = "family"
	}
	if s.settings.IntervalSec < 5 {
		s.settings.IntervalSec = 5
	}
	if s.settings.IntervalSec > 3600 {
		s.settings.IntervalSec = 3600
	}
	switch s.settings.Order {
	case "shuffle", "newest", "chronological":
	default:
		s.settings.Order = "shuffle"
	}
	switch s.settings.Transition {
	case "fade", "kenburns", "slide":
	default:
		s.settings.Transition = "kenburns"
	}
	switch s.settings.FitMode {
	case "ambient", "fill", "matte":
	default:
		s.settings.FitMode = "ambient"
	}
	if s.settings.PublicCategory == "" {
		s.settings.PublicCategory = "all"
	}
	if s.settings.PublicSource == "" {
		s.settings.PublicSource = "all"
	}
	if s.settings.Brightness < 0 {
		s.settings.Brightness = 0
	}
	if s.settings.Brightness > 255 {
		s.settings.Brightness = 255
	}
	if strings.TrimSpace(s.settings.FamilyToken) == "" {
		s.settings.FamilyToken = generateFriendlyToken()
	}
}

func (s *Store) AddPhoto(p *Photo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == "" {
		p.ID = NewID()
	}
	if p.UploadedAt.IsZero() {
		p.UploadedAt = time.Now().UTC()
	}
	if p.Source == "" {
		p.Source = "family"
	}
	s.photos = append(s.photos, p)
	s.byID[p.ID] = p
	return s.saveLibraryLocked()
}

func (s *Store) HasPublicExternalID(extID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, p := range s.photos {
		if p.Source == "public" && p.PublicMeta != nil && p.PublicMeta.ExternalID == extID {
			return true
		}
	}
	return false
}

func (s *Store) GetPhoto(id string) (*Photo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (s *Store) UpdatePhoto(id string, fn func(*Photo)) (*Photo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	fn(p)
	if err := s.saveLibraryLocked(); err != nil {
		return nil, err
	}
	cp := *p
	return &cp, nil
}

func (s *Store) DeletePhoto(id string) (*Photo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	delete(s.byID, id)
	next := make([]*Photo, 0, len(s.photos)-1)
	for _, item := range s.photos {
		if item.ID != id {
			next = append(next, item)
		}
	}
	s.photos = next
	if err := s.saveLibraryLocked(); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) PrunePublicPhotos(maxCount int) []*Photo {
	s.mu.Lock()
	defer s.mu.Unlock()
	var pub []*Photo
	for _, p := range s.photos {
		if p.Source == "public" && !p.Favorite {
			pub = append(pub, p)
		}
	}
	if len(pub) <= maxCount {
		return nil
	}
	sort.Slice(pub, func(i, j int) bool {
		return pub[i].UploadedAt.Before(pub[j].UploadedAt)
	})
	toRemove := pub[:len(pub)-maxCount]
	rmSet := make(map[string]bool, len(toRemove))
	for _, p := range toRemove {
		rmSet[p.ID] = true
		delete(s.byID, p.ID)
	}
	next := make([]*Photo, 0, len(s.photos)-len(toRemove))
	for _, p := range s.photos {
		if !rmSet[p.ID] {
			next = append(next, p)
		}
	}
	s.photos = next
	_ = s.saveLibraryLocked()
	return toRemove
}

func (s *Store) ListAll(sourceFilter string) []*Photo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Photo, 0, len(s.photos))
	for _, p := range s.photos {
		if sourceFilter != "" && p.Source != sourceFilter {
			continue
		}
		cp := *p
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].UploadedAt.After(out[j].UploadedAt)
	})
	return out
}

func (s *Store) Albums() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := make(map[string]bool)
	for _, p := range s.photos {
		if p.Source == "family" && !p.Hidden && strings.TrimSpace(p.Album) != "" {
			set[strings.TrimSpace(p.Album)] = true
		}
	}
	albums := make([]string, 0, len(set))
	for a := range set {
		albums = append(albums, a)
	}
	sort.Strings(albums)
	return albums
}

func matchesPublicFilter(p *Photo, filter string) bool {
	if filter == "" || filter == "all" {
		return true
	}
	if p.PublicMeta == nil {
		return false
	}
	if strings.EqualFold(p.PublicMeta.Category, filter) {
		return true
	}
	if strings.EqualFold(p.PublicMeta.Provider, filter) {
		return true
	}
	return false
}

func (s *Store) PublicCategories() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := make(map[string]bool)
	for _, p := range s.photos {
		if p.Source == "public" && !p.Hidden && p.PublicMeta != nil && p.PublicMeta.Category != "" {
			set[p.PublicMeta.Category] = true
		}
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func (s *Store) SlideshowPlaylist(mode, album, publicFilter, order string) []*Photo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if mode == "" {
		mode = s.settings.Mode
	}
	if order == "" {
		order = s.settings.Order
	}
	if publicFilter == "" {
		publicFilter = s.settings.PublicCategory
		if publicFilter == "" || publicFilter == "all" {
			publicFilter = s.settings.PublicSource
		}
	}

	out := make([]*Photo, 0, len(s.photos))
	for _, p := range s.photos {
		if p.Hidden {
			continue
		}
		switch mode {
		case "family":
			if p.Source != "family" {
				continue
			}
			if album == "favorites" {
				if !p.Favorite {
					continue
				}
			} else if album != "" && !strings.EqualFold(p.Album, album) {
				continue
			}
		case "public":
			if p.Source != "public" {
				continue
			}
			if !matchesPublicFilter(p, publicFilter) {
				continue
			}
		case "mix":
			if p.Source == "family" && album == "favorites" && !p.Favorite {
				continue
			}
			if p.Source == "family" && album != "" && album != "favorites" && !strings.EqualFold(p.Album, album) {
				continue
			}
			if p.Source == "public" && !matchesPublicFilter(p, publicFilter) {
				continue
			}
		}
		cp := *p
		out = append(out, &cp)
	}

	switch order {
	case "newest":
		sort.Slice(out, func(i, j int) bool {
			return photoSortTime(out[i]).After(photoSortTime(out[j]))
		})
	case "chronological":
		sort.Slice(out, func(i, j int) bool {
			return photoSortTime(out[i]).Before(photoSortTime(out[j]))
		})
	case "shuffle":
		mrand.Shuffle(len(out), func(i, j int) {
			out[i], out[j] = out[j], out[i]
		})
	}

	return out
}

func photoSortTime(p *Photo) time.Time {
	if p.Exif != nil && p.Exif.TakenAt != "" {
		if t, err := time.Parse(time.RFC3339, p.Exif.TakenAt); err == nil {
			return t
		}
	}
	return p.UploadedAt
}

type Counts struct {
	Family    int `json:"family"`
	Public    int `json:"public"`
	Favorites int `json:"favorites"`
	Hidden    int `json:"hidden"`
}

func (s *Store) Counts() Counts {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var c Counts
	for _, p := range s.photos {
		if p.Hidden {
			c.Hidden++
			continue
		}
		if p.Source == "public" {
			c.Public++
		} else {
			c.Family++
		}
		if p.Favorite {
			c.Favorites++
		}
	}
	return c
}

func (s *Store) saveLibraryLocked() error {
	return atomicWriteJSON(filepath.Join(s.dataDir, "library.json"), Library{Photos: s.photos})
}

func (s *Store) saveSettingsLocked() error {
	return atomicWriteJSON(filepath.Join(s.dataDir, "settings.json"), s.settings)
}

func atomicWriteJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
