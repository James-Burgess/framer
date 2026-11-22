package publicfeed

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"frmaeo/internal/media"
	"frmaeo/internal/store"
)

const (
	maxPublicCached = 64
	maxDownloadSize = 18 << 20 // 18 MB
)

type CategoryInfo struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

var BuiltinCategories = []CategoryInfo{
	{ID: "all", Label: "All Curated"},
	{ID: "fine_art", Label: "Fine Art"},
	{ID: "nature", Label: "Nature & Wildlife"},
	{ID: "space", Label: "Space & Cosmos"},
	{ID: "architecture", Label: "Architecture"},
	{ID: "vintage", Label: "Vintage & History"},
}

type Manager struct {
	store      *store.Store
	client     *http.Client
	onNewPhoto func(*store.Photo)
	onProgress func(Status)

	mu          sync.Mutex
	refreshing  bool
	done        int
	total       int
	currentItem string
	category    string
	lastSync    time.Time
	lastError   string
}

type Status struct {
	Refreshing  bool   `json:"refreshing"`
	Done        int    `json:"done"`
	Total       int    `json:"total"`
	Percent     int    `json:"percent"`
	CurrentItem string `json:"currentItem,omitempty"`
	Category    string `json:"category,omitempty"`
	LastSync    string `json:"lastSync,omitempty"`
	LastError   string `json:"lastError,omitempty"`
	Cached      int    `json:"cached"`
}

func NewManager(st *store.Store, onNewPhoto func(*store.Photo), onProgress func(Status)) *Manager {
	return &Manager{
		store:      st,
		client:     newAndroidSafeHTTPClient(),
		onNewPhoto: onNewPhoto,
		onProgress: onProgress,
	}
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.statusLocked()
}

func (m *Manager) statusLocked() Status {
	pct := 0
	if m.total > 0 {
		pct = (m.done * 100) / m.total
		if pct > 100 {
			pct = 100
		}
	}
	st := Status{
		Refreshing:  m.refreshing,
		Done:        m.done,
		Total:       m.total,
		Percent:     pct,
		CurrentItem: m.currentItem,
		Category:    m.category,
		LastError:   m.lastError,
		Cached:      m.store.Counts().Public,
	}
	if !m.lastSync.IsZero() {
		st.LastSync = m.lastSync.Format(time.RFC3339)
	}
	return st
}

func (m *Manager) setProgress(done, total int, currentItem string) {
	m.mu.Lock()
	if done >= 0 {
		m.done = done
	}
	if total >= 0 {
		m.total = total
	}
	m.currentItem = currentItem
	st := m.statusLocked()
	m.mu.Unlock()
	if m.onProgress != nil {
		m.onProgress(st)
	}
}

func (m *Manager) bumpDone(currentItem string) {
	m.mu.Lock()
	m.done++
	m.currentItem = currentItem
	st := m.statusLocked()
	m.mu.Unlock()
	if m.onProgress != nil {
		m.onProgress(st)
	}
}

// RefreshAsync kicks off a non-blocking category fetch.
func (m *Manager) RefreshAsync(category string) bool {
	if category == "" {
		category = "all"
	}
	m.mu.Lock()
	if m.refreshing {
		m.mu.Unlock()
		return false
	}
	m.refreshing = true
	m.done = 0
	m.total = 12
	m.currentItem = "Connecting to public galleries..."
	m.category = category
	m.lastError = ""
	st := m.statusLocked()
	m.mu.Unlock()
	if m.onProgress != nil {
		m.onProgress(st)
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		err := m.refreshCategory(ctx, category)

		m.mu.Lock()
		m.refreshing = false
		if err != nil {
			m.lastError = err.Error()
			m.currentItem = ""
		} else {
			m.done = m.total
			m.currentItem = "Done"
			m.lastSync = time.Now().UTC()
		}
		finalSt := m.statusLocked()
		m.mu.Unlock()
		if m.onProgress != nil {
			m.onProgress(finalSt)
		}
	}()
	return true
}

// SearchAsync searches Wikimedia Commons, The Met Museum, and NASA for a topic
// and caches up to 10 matching photos tagged with category "search:<query>".
func (m *Manager) SearchAsync(query string) bool {
	q := strings.TrimSpace(query)
	if q == "" {
		return false
	}
	catKey := "search:" + strings.ToLower(q)

	m.mu.Lock()
	if m.refreshing {
		m.mu.Unlock()
		return false
	}
	m.refreshing = true
	m.done = 0
	m.total = 9
	m.currentItem = fmt.Sprintf("Searching \"%s\"...", q)
	m.category = catKey
	m.lastError = ""
	st := m.statusLocked()
	m.mu.Unlock()
	if m.onProgress != nil {
		m.onProgress(st)
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		var added int
		var firstErr error

		n1, err1 := m.searchWikimedia(ctx, q, catKey, 5)
		added += n1
		if err1 != nil && firstErr == nil {
			firstErr = err1
		}

		n2, err2 := m.searchNASA(ctx, q, catKey, 3)
		added += n2
		if err2 != nil && firstErr == nil {
			firstErr = err2
		}

		n3, err3 := m.searchMet(ctx, q, catKey, 3)
		added += n3
		if err3 != nil && firstErr == nil {
			firstErr = err3
		}

		removed := m.store.PrunePublicPhotos(maxPublicCached)
		for _, p := range removed {
			media.RemoveFiles(m.store, p)
		}

		m.mu.Lock()
		m.refreshing = false
		if added == 0 && firstErr != nil {
			m.lastError = firstErr.Error()
			m.currentItem = ""
		} else if added == 0 {
			m.lastError = fmt.Sprintf("No images found for \"%s\"", q)
			m.currentItem = ""
		} else {
			m.done = m.total
			m.currentItem = fmt.Sprintf("Found %d photos for \"%s\"", added, q)
			m.lastSync = time.Now().UTC()
		}
		finalSt := m.statusLocked()
		m.mu.Unlock()
		if m.onProgress != nil {
			m.onProgress(finalSt)
		}
	}()
	return true
}

func (m *Manager) refreshCategory(ctx context.Context, category string) error {
	var added int
	var firstErr error

	// If it's a search category, re-run search
	if strings.HasPrefix(category, "search:") {
		q := strings.TrimPrefix(category, "search:")
		n1, e1 := m.searchWikimedia(ctx, q, category, 5)
		n2, e2 := m.searchNASA(ctx, q, category, 3)
		added = n1 + n2
		if added == 0 {
			if e1 != nil {
				return e1
			}
			return e2
		}
		return nil
	}

	switch category {
	case "fine_art", "met":
		m.setProgress(0, 8, "Fetching masterpieces from The Met Museum...")
		n, err := m.fetchMetCurated(ctx, "fine_art", metFineArtIDs, 8)
		added += n
		firstErr = err
	case "nature", "wikimedia":
		m.setProgress(0, 8, "Fetching nature & wildlife from Wikimedia...")
		n, err := m.searchWikimedia(ctx, "landscape mountain wildlife sunset nature", "nature", 8)
		added += n
		firstErr = err
	case "space", "nasa":
		m.setProgress(0, 8, "Fetching space photography from NASA...")
		n, err := m.searchNASA(ctx, "nebula galaxy earth hubble webb", "space", 8)
		added += n
		firstErr = err
	case "architecture":
		m.setProgress(0, 8, "Fetching architecture & landmarks...")
		n, err := m.searchWikimedia(ctx, "historic architecture cathedral palace bridge", "architecture", 8)
		added += n
		firstErr = err
	case "vintage":
		m.setProgress(0, 8, "Fetching vintage photography & archives...")
		n1, e1 := m.fetchMetCurated(ctx, "vintage", metVintageIDs, 4)
		n2, e2 := m.searchNASA(ctx, "apollo 11 moon landing", "vintage", 4)
		added += n1 + n2
		if e1 != nil {
			firstErr = e1
		} else {
			firstErr = e2
		}
	default: // "all"
		m.setProgress(0, 12, "Fetching curated gallery (Fine Art, Nature, Space)...")
		n1, e1 := m.fetchMetCurated(ctx, "fine_art", metFineArtIDs, 4)
		added += n1
		if e1 != nil && firstErr == nil {
			firstErr = e1
		}
		n2, e2 := m.searchWikimedia(ctx, "landscape mountain sunset coastline", "nature", 4)
		added += n2
		if e2 != nil && firstErr == nil {
			firstErr = e2
		}
		n3, e3 := m.searchNASA(ctx, "nebula galaxy earth orbit", "space", 4)
		added += n3
		if e3 != nil && firstErr == nil {
			firstErr = e3
		}
	}

	removed := m.store.PrunePublicPhotos(maxPublicCached)
	for _, p := range removed {
		media.RemoveFiles(m.store, p)
	}

	if added == 0 && firstErr != nil {
		return firstErr
	}
	return nil
}

// Curated iconic public-domain masterpieces from The Met Museum Open Access.
var metFineArtIDs = []int{
	436535, // Van Gogh - Wheat Field with Cypresses
	437879, // Vermeer - Young Woman with a Water Pitcher
	436524, // Van Gogh - Sunflowers
	437133, // Monet - Bridge over a Pond of Water Lilies
	459123, // Hokusai - The Great Wave off Kanagawa
	436965, // Monet - Garden at Sainte-Adresse
	436121, // Degas - The Dance Class
	437430, // Rembrandt - Aristotle with a Bust of Homer
	438817, // Sargent - Madame X
	11417,  // Bierstadt - The Rocky Mountains, Lander's Peak
	10481,  // Cole - The Oxbow
	435882, // Bruegel - The Harvesters
}

var metVintageIDs = []int{
	283101, // Stieglitz / early photography
	265758, // Carleton Watkins - Yosemite
	269566, // Early landscape photography
	286240, // Vintage architectural study
	266814, // Historic photography
	263223, // Classic print
}

func (m *Manager) fetchMetCurated(ctx context.Context, category string, ids []int, limit int) (int, error) {
	added := 0
	var lastErr error
	for _, objID := range ids {
		if added >= limit {
			break
		}
		extID := fmt.Sprintf("met:%d", objID)
		if m.store.HasPublicExternalID(extID) {
			m.bumpDone("Already cached (The Met)")
			added++
			continue
		}
		ok, err := m.fetchMetObject(ctx, objID, category, "")
		if err != nil {
			lastErr = err
			continue
		}
		if ok {
			added++
		}
	}
	if added == 0 && lastErr != nil {
		return 0, fmt.Errorf("met museum: %w", lastErr)
	}
	return added, nil
}

func (m *Manager) searchMet(ctx context.Context, query, category string, limit int) (int, error) {
	apiURL := "https://collectionapi.metmuseum.org/public/collection/v1/search?hasImages=true&q=" + url.QueryEscape(query)
	var res struct {
		ObjectIDs []int `json:"objectIDs"`
	}
	if err := m.getJSON(ctx, apiURL, &res); err != nil {
		return 0, fmt.Errorf("met search: %w", err)
	}
	added := 0
	for i, objID := range res.ObjectIDs {
		if added >= limit || i >= 15 {
			break
		}
		extID := fmt.Sprintf("met:%d", objID)
		if m.store.HasPublicExternalID(extID) {
			continue
		}
		ok, _ := m.fetchMetObject(ctx, objID, category, query)
		if ok {
			added++
		}
	}
	return added, nil
}

func (m *Manager) fetchMetObject(ctx context.Context, objID int, category, searchQuery string) (bool, error) {
	apiURL := fmt.Sprintf("https://collectionapi.metmuseum.org/public/collection/v1/objects/%d", objID)
	var obj struct {
		ObjectID          int    `json:"objectID"`
		Title             string `json:"title"`
		ArtistDisplayName string `json:"artistDisplayName"`
		ObjectDate        string `json:"objectDate"`
		Medium            string `json:"medium"`
		Department        string `json:"department"`
		PrimaryImage      string `json:"primaryImage"`
		PrimaryImageSmall string `json:"primaryImageSmall"`
		ObjectURL         string `json:"objectURL"`
	}
	if err := m.getJSON(ctx, apiURL, &obj); err != nil {
		return false, err
	}
	imgURL := obj.PrimaryImageSmall
	if imgURL == "" {
		imgURL = obj.PrimaryImage
	}
	if imgURL == "" {
		return false, nil
	}

	title := strings.TrimSpace(obj.Title)
	m.setProgress(-1, -1, fmt.Sprintf("Downloading: %s", truncate(title, 38)))

	desc := strings.TrimSpace(obj.Medium)
	if obj.Department != "" {
		if desc != "" {
			desc += " · " + obj.Department
		} else {
			desc = obj.Department
		}
	}

	err := m.downloadAndStore(ctx, imgURL, &store.PublicMeta{
		Provider:    "met",
		Category:    category,
		SearchQuery: searchQuery,
		Title:       title,
		Artist:      strings.TrimSpace(obj.ArtistDisplayName),
		Date:        strings.TrimSpace(obj.ObjectDate),
		Description: desc,
		SourceURL:   obj.ObjectURL,
		ExternalID:  fmt.Sprintf("met:%d", objID),
	})
	if err != nil {
		return false, err
	}
	m.bumpDone(fmt.Sprintf("Saved: %s", truncate(title, 38)))
	return true, nil
}

// flexString safely unmarshals JSON strings, numbers, booleans, or nulls into a string.
// Wikimedia's extmetadata API frequently mixes strings, numbers, and booleans in "value".
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	trimmed := strings.TrimSpace(string(b))
	if trimmed == "" || trimmed == "null" {
		*f = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var num json.Number
	if err := json.Unmarshal(b, &num); err == nil {
		*f = flexString(num.String())
		return nil
	}
	var bv bool
	if err := json.Unmarshal(b, &bv); err == nil {
		if bv {
			*f = "true"
		} else {
			*f = "false"
		}
		return nil
	}
	*f = flexString(trimmed)
	return nil
}

func (m *Manager) searchWikimedia(ctx context.Context, query, category string, limit int) (int, error) {
	q := url.QueryEscape(query + " filetype:bitmap")
	gsrLimit := limit*3 + 6
	if gsrLimit > 30 {
		gsrLimit = 30
	}
	apiURL := fmt.Sprintf(
		"https://commons.wikimedia.org/w/api.php?action=query&generator=search&gsrnamespace=6&gsrsearch=%s&gsrlimit=%d&prop=imageinfo&iiprop=url|extmetadata|mime|size&iiurlwidth=1280&format=json",
		q, gsrLimit,
	)
	var resp struct {
		Query struct {
			Pages map[string]struct {
				PageID    int    `json:"pageid"`
				Title     string `json:"title"`
				ImageInfo []struct {
					ThumbURL    string `json:"thumburl"`
					URL         string `json:"url"`
					Mime        string `json:"mime"`
					Width       int    `json:"width"`
					Height      int    `json:"height"`
					ExtMetadata map[string]struct {
						Value flexString `json:"value"`
					} `json:"extmetadata"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := m.getJSON(ctx, apiURL, &resp); err != nil {
		return 0, fmt.Errorf("wikimedia: %w", err)
	}

	added := 0
	var lastErr error
	for _, page := range resp.Query.Pages {
		if added >= limit {
			break
		}
		if len(page.ImageInfo) == 0 {
			continue
		}
		info := page.ImageInfo[0]
		if info.Mime != "" && !strings.HasPrefix(info.Mime, "image/jpeg") && !strings.HasPrefix(info.Mime, "image/png") && !strings.HasPrefix(info.Mime, "image/webp") {
			continue
		}
		if info.Width > 0 && info.Height > 0 && (info.Width < 500 || info.Height < 350) {
			continue
		}
		imgURL := info.ThumbURL
		if imgURL == "" {
			imgURL = info.URL
		}
		if imgURL == "" {
			continue
		}
		extID := fmt.Sprintf("wikimedia:%d", page.PageID)
		if m.store.HasPublicExternalID(extID) {
			m.bumpDone("Already cached (Wikimedia)")
			added++
			continue
		}

		title := cleanHTML(string(info.ExtMetadata["ObjectName"].Value))
		if title == "" {
			title = strings.TrimPrefix(page.Title, "File:")
			if idx := strings.LastIndex(title, "."); idx > 0 {
				title = title[:idx]
			}
			title = strings.ReplaceAll(title, "_", " ")
		}
		artist := cleanHTML(string(info.ExtMetadata["Artist"].Value))
		desc := cleanHTML(string(info.ExtMetadata["ImageDescription"].Value))
		date := cleanHTML(string(info.ExtMetadata["DateTimeOriginal"].Value))
		if len(desc) > 220 {
			desc = desc[:217] + "..."
		}

		m.setProgress(-1, -1, fmt.Sprintf("Downloading: %s", truncate(title, 38)))
		searchQ := ""
		if strings.HasPrefix(category, "search:") {
			searchQ = query
		}
		meta := &store.PublicMeta{
			Provider:    "wikimedia",
			Category:    category,
			SearchQuery: searchQ,
			Title:       title,
			Artist:      artist,
			Date:        date,
			Description: desc,
			SourceURL:   "https://commons.wikimedia.org/wiki/" + url.PathEscape(page.Title),
			ExternalID:  extID,
		}
		err := m.downloadAndStore(ctx, imgURL, meta)
		if err != nil && info.URL != "" && info.URL != imgURL {
			// Fallback to full URL if thumbnail endpoint rate-limited or failed
			err = m.downloadAndStore(ctx, info.URL, meta)
		}
		if err == nil {
			added++
			m.bumpDone(fmt.Sprintf("Saved: %s", truncate(title, 38)))
		} else {
			lastErr = err
		}
	}
	if added == 0 && lastErr != nil {
		return 0, fmt.Errorf("wikimedia download: %w", lastErr)
	}
	return added, nil
}

func (m *Manager) searchNASA(ctx context.Context, query, category string, limit int) (int, error) {
	apiURL := fmt.Sprintf("https://images-api.nasa.gov/search?q=%s&media_type=image&page_size=%d", url.QueryEscape(query), limit*2+6)
	var resp struct {
		Collection struct {
			Items []struct {
				Data []struct {
					NasaID      string `json:"nasa_id"`
					Title       string `json:"title"`
					Description string `json:"description"`
					DateCreated string `json:"date_created"`
					Center      string `json:"center"`
				} `json:"data"`
				Links []struct {
					Href string `json:"href"`
					Rel  string `json:"rel"`
				} `json:"links"`
			} `json:"items"`
		} `json:"collection"`
	}
	if err := m.getJSON(ctx, apiURL, &resp); err != nil {
		return 0, fmt.Errorf("nasa: %w", err)
	}

	added := 0
	var lastErr error
	for _, item := range resp.Collection.Items {
		if added >= limit {
			break
		}
		if len(item.Data) == 0 || len(item.Links) == 0 {
			continue
		}
		d := item.Data[0]
		var previewURL string
		for _, lk := range item.Links {
			if lk.Rel == "preview" && lk.Href != "" {
				previewURL = lk.Href
				break
			}
		}
		if previewURL == "" {
			previewURL = item.Links[0].Href
		}
		if previewURL == "" {
			continue
		}
		extID := "nasa:" + d.NasaID
		if m.store.HasPublicExternalID(extID) {
			m.bumpDone("Already cached (NASA)")
			added++
			continue
		}

		title := strings.TrimSpace(d.Title)
		desc := strings.TrimSpace(d.Description)
		if len(desc) > 220 {
			desc = desc[:217] + "..."
		}
		dateStr := d.DateCreated
		if len(dateStr) >= 10 {
			dateStr = dateStr[:10]
		}
		artist := "NASA"
		if d.Center != "" {
			artist = "NASA · " + d.Center
		}

		m.setProgress(-1, -1, fmt.Sprintf("Downloading: %s", truncate(title, 38)))
		searchQ := ""
		if strings.HasPrefix(category, "search:") {
			searchQ = query
		}
		meta := &store.PublicMeta{
			Provider:    "nasa",
			Category:    category,
			SearchQuery: searchQ,
			Title:       title,
			Artist:      artist,
			Date:        dateStr,
			Description: desc,
			SourceURL:   "https://images.nasa.gov/details/" + url.PathEscape(d.NasaID),
			ExternalID:  extID,
		}

		// Try higher-res ~medium.jpg first, fall back to preview (~thumb.jpg)
		var err error
		if strings.Contains(previewURL, "~thumb.jpg") {
			medURL := strings.Replace(previewURL, "~thumb.jpg", "~medium.jpg", 1)
			err = m.downloadAndStore(ctx, medURL, meta)
			if err != nil {
				err = m.downloadAndStore(ctx, previewURL, meta)
			}
		} else {
			err = m.downloadAndStore(ctx, previewURL, meta)
		}

		if err == nil {
			added++
			m.bumpDone(fmt.Sprintf("Saved: %s", truncate(title, 38)))
		} else {
			lastErr = err
		}
	}
	if added == 0 && lastErr != nil {
		return 0, fmt.Errorf("nasa download: %w", lastErr)
	}
	return added, nil
}

func (m *Manager) downloadAndStore(ctx context.Context, imgURL string, meta *store.PublicMeta) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "TheJimmyFrame/1.0 (https://github.com/frmaeo; personal-digital-photo-frame)")
	req.Header.Set("Accept", "image/*,*/*;q=0.8")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSize))
	if err != nil {
		return err
	}

	id := store.NewID()
	res, err := media.IngestBytes(m.store, id, ".jpg", raw)
	if err != nil {
		return err
	}

	photo := &store.Photo{
		ID:         id,
		Filename:   id + ".jpg",
		Source:     "public",
		Caption:    meta.Title,
		Sender:     meta.Artist,
		Width:      res.Width,
		Height:     res.Height,
		SizeBytes:  res.SizeBytes,
		UploadedAt: time.Now().UTC(),
		Exif:       res.Exif,
		PublicMeta: meta,
	}
	if err := m.store.AddPhoto(photo); err != nil {
		media.RemoveFiles(m.store, photo)
		return err
	}
	if m.onNewPhoto != nil {
		m.onNewPhoto(photo)
	}
	return nil
}

func (m *Manager) getJSON(ctx context.Context, u string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "TheJimmyFrame/1.0 (https://github.com/frmaeo; personal-digital-photo-frame)")
	req.Header.Set("Accept", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(dst)
}

var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

func cleanHTML(s string) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
