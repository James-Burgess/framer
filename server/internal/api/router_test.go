package api_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"frmaeo/internal/api"
	"frmaeo/internal/media"
	"frmaeo/internal/store"
)

func makeTestJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8((x * 255) / w), G: uint8((y * 255) / h), B: 140, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func newTestServer(t *testing.T) (*api.Server, *store.Store, http.Handler) {
	t.Helper()
	st, err := store.Open(t.TempDir(), "secret-1234")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	webFS := fstest.MapFS{
		"web/kiosk/index.html":  {Data: []byte("<html>kiosk</html>")},
		"web/manage/index.html": {Data: []byte("<html>manage</html>")},
	}
	srv := api.NewServer(st, webFS, 8080)
	return srv, st, srv.Handler()
}

func TestGeocodeOffline(t *testing.T) {
	if got := media.LookupLocation(-33.9249, 18.4241); got != "Cape Town, South Africa" {
		t.Fatalf("expected Cape Town, got %q", got)
	}
	if got := media.LookupLocation(0.0, -140.0); !strings.Contains(got, "140.00° W") {
		t.Fatalf("expected formatted coords for remote ocean, got %q", got)
	}
}

func TestAuthAndUploadLifecycle(t *testing.T) {
	_, st, h := newTestServer(t)

	// 1. Health check
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("health status %d", rec.Code)
	}

	// 2. Manage list without token => 401
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/manage/photos", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}

	// 3. Upload a family photo with valid token
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("caption", "Sunset at Clifton")
	_ = mw.WriteField("sender", "James")
	_ = mw.WriteField("album", "Summer")
	fw, _ := mw.CreateFormFile("files", "sunset.jpg")
	_, _ = fw.Write(makeTestJPEG(t, 640, 480))
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/manage/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Frame-Token", "secret-1234")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
	}

	var upResp struct {
		Uploaded []store.Photo `json:"uploaded"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &upResp); err != nil || len(upResp.Uploaded) != 1 {
		t.Fatalf("unexpected upload resp: %s", rec.Body.String())
	}
	photoID := upResp.Uploaded[0].ID

	// 4. Verify display and thumb images serve
	for _, kind := range []string{"display", "thumb"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/photos/"+photoID+"/"+kind, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 for %s, got %d", kind, rec.Code)
		}
	}

	// 5. Rotate 90° swaps 640x480 => 480x640
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/photos/"+photoID+"/rotate", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate failed: %d", rec.Code)
	}
	var rotated store.Photo
	_ = json.Unmarshal(rec.Body.Bytes(), &rotated)
	if rotated.Width != 480 || rotated.Height != 640 || rotated.Rotation != 90 {
		t.Fatalf("unexpected rotated dims: %+v", rotated)
	}

	// 6. Favorite toggle
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/photos/"+photoID+"/favorite", strings.NewReader("{}")))
	if rec.Code != http.StatusOK {
		t.Fatalf("favorite failed: %d", rec.Code)
	}

	// 7. Slideshow returns photo in family mode and favorites filter
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/slideshow?mode=family&album=favorites", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), photoID) {
		t.Fatalf("slideshow missing favorite photo: %d %s", rec.Code, rec.Body.String())
	}

	// 8. QR SVG generation
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/qr.svg", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<svg") {
		t.Fatalf("qr svg failed: %d", rec.Code)
	}

	// 9. Verify store persistence across reopen
	reopened, err := store.Open(st.DataDir(), "")
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	p, err := reopened.GetPhoto(photoID)
	if err != nil || !p.Favorite || p.Album != "Summer" {
		t.Fatalf("persisted photo mismatch: %+v err=%v", p, err)
	}
}
