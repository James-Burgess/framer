package media

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/skip2/go-qrcode"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"frmaeo/internal/store"
)

const (
	MaxDisplayDim = 1600
	MaxThumbDim   = 420
)

type ProcessedResult struct {
	Width     int
	Height    int
	SizeBytes int64
	Exif      *store.ExifData
}

// IngestBytes writes the original file, extracts EXIF, normalizes orientation,
// and generates display (1600px) and thumbnail (420px) JPEGs.
func IngestBytes(st *store.Store, id, origExt string, raw []byte) (*ProcessedResult, error) {
	if origExt == "" {
		origExt = ".jpg"
	}
	if !strings.HasPrefix(origExt, ".") {
		origExt = "." + origExt
	}
	origExt = strings.ToLower(origExt)

	origPath := filepath.Join(st.OriginalsDir(), id+origExt)
	if err := os.WriteFile(origPath, raw, 0o644); err != nil {
		return nil, fmt.Errorf("write original: %w", err)
	}

	exifData := ExtractExif(raw)

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		_ = os.Remove(origPath)
		return nil, fmt.Errorf("decode image: %w", err)
	}

	if exifData != nil && exifData.Orientation > 1 {
		img = applyExifOrientation(img, exifData.Orientation)
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	if err := writeVariants(st, id, img); err != nil {
		_ = os.Remove(origPath)
		return nil, err
	}

	return &ProcessedResult{
		Width:     w,
		Height:    h,
		SizeBytes: int64(len(raw)),
		Exif:      exifData,
	}, nil
}

// Rotate90CW rotates the display and thumb variants 90 degrees clockwise.
func Rotate90CW(st *store.Store, id string) (int, int, error) {
	dispPath := filepath.Join(st.DisplayDir(), id+".jpg")
	f, err := os.Open(dispPath)
	if err != nil {
		return 0, 0, err
	}
	img, _, err := image.Decode(f)
	_ = f.Close()
	if err != nil {
		return 0, 0, err
	}

	rot := rotate90(img)
	if err := writeVariants(st, id, rot); err != nil {
		return 0, 0, err
	}
	b := rot.Bounds()
	return b.Dx(), b.Dy(), nil
}

// RemoveFiles deletes original, display, and thumb files for a photo ID.
func RemoveFiles(st *store.Store, p *store.Photo) {
	ext := strings.ToLower(filepath.Ext(p.Filename))
	if ext == "" {
		ext = ".jpg"
	}
	_ = os.Remove(filepath.Join(st.OriginalsDir(), p.ID+ext))
	_ = os.Remove(filepath.Join(st.DisplayDir(), p.ID+".jpg"))
	_ = os.Remove(filepath.Join(st.ThumbsDir(), p.ID+".jpg"))
}

func writeVariants(st *store.Store, id string, img image.Image) error {
	dispImg := resizeFit(img, MaxDisplayDim)
	if err := saveJPEG(filepath.Join(st.DisplayDir(), id+".jpg"), dispImg, 86); err != nil {
		return fmt.Errorf("write display: %w", err)
	}
	thumbImg := resizeFit(dispImg, MaxThumbDim)
	if err := saveJPEG(filepath.Join(st.ThumbsDir(), id+".jpg"), thumbImg, 80); err != nil {
		return fmt.Errorf("write thumb: %w", err)
	}
	return nil
}

func saveJPEG(path string, img image.Image, quality int) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: quality}); err != nil {
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

func resizeFit(src image.Image, maxDim int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= maxDim && h <= maxDim {
		return src
	}
	var nw, nh int
	if w >= h {
		nw = maxDim
		nh = int(math.Round(float64(h) * float64(maxDim) / float64(w)))
	} else {
		nh = maxDim
		nw = int(math.Round(float64(w) * float64(maxDim) / float64(h)))
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}

func ExtractExif(raw []byte) *store.ExifData {
	x, err := exif.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil
	}
	data := &store.ExifData{}
	hasAny := false

	if tm, err := x.DateTime(); err == nil && !tm.IsZero() {
		data.TakenAt = tm.Format(time.RFC3339)
		hasAny = true
	}
	if tag, err := x.Get(exif.Make); err == nil {
		if v, err := tag.StringVal(); err == nil {
			data.CameraMake = strings.TrimSpace(strings.Trim(v, "\x00"))
			hasAny = hasAny || data.CameraMake != ""
		}
	}
	if tag, err := x.Get(exif.Model); err == nil {
		if v, err := tag.StringVal(); err == nil {
			data.CameraModel = strings.TrimSpace(strings.Trim(v, "\x00"))
			hasAny = hasAny || data.CameraModel != ""
		}
	}
	if tag, err := x.Get(exif.LensModel); err == nil {
		if v, err := tag.StringVal(); err == nil {
			data.Lens = strings.TrimSpace(strings.Trim(v, "\x00"))
			hasAny = hasAny || data.Lens != ""
		}
	}
	if tag, err := x.Get(exif.FocalLength); err == nil {
		if num, denom, err := tag.Rat2(0); err == nil && denom != 0 {
			fl := float64(num) / float64(denom)
			if math.Abs(fl-math.Round(fl)) < 0.05 {
				data.FocalLength = fmt.Sprintf("%.0fmm", fl)
			} else {
				data.FocalLength = fmt.Sprintf("%.1fmm", fl)
			}
			hasAny = true
		}
	}
	if tag, err := x.Get(exif.FNumber); err == nil {
		if num, denom, err := tag.Rat2(0); err == nil && denom != 0 {
			fn := float64(num) / float64(denom)
			data.Aperture = fmt.Sprintf("f/%.1f", fn)
			hasAny = true
		}
	}
	if tag, err := x.Get(exif.ExposureTime); err == nil {
		if num, denom, err := tag.Rat2(0); err == nil && denom != 0 {
			if num == 0 {
				// skip
			} else if num < denom {
				inv := int(math.Round(float64(denom) / float64(num)))
				if inv > 1 {
					data.Shutter = fmt.Sprintf("1/%ds", inv)
				} else {
					data.Shutter = fmt.Sprintf("%.2fs", float64(num)/float64(denom))
				}
			} else {
				data.Shutter = fmt.Sprintf("%.1fs", float64(num)/float64(denom))
			}
			hasAny = hasAny || data.Shutter != ""
		}
	}
	if tag, err := x.Get(exif.ISOSpeedRatings); err == nil {
		if iso, err := tag.Int(0); err == nil && iso > 0 {
			data.ISO = iso
			hasAny = true
		}
	}
	if tag, err := x.Get(exif.Orientation); err == nil {
		if ori, err := tag.Int(0); err == nil {
			data.Orientation = ori
		}
	}
	if lat, lon, err := x.LatLong(); err == nil && !math.IsNaN(lat) && !math.IsNaN(lon) {
		if lat != 0 || lon != 0 {
			data.Lat = lat
			data.Lon = lon
			data.HasGPS = true
			data.LocationName = LookupLocation(lat, lon)
			hasAny = true
		}
	}

	if !hasAny && data.Orientation <= 1 {
		return nil
	}
	return data
}

func applyExifOrientation(src image.Image, orientation int) image.Image {
	switch orientation {
	case 2:
		return flipH(src)
	case 3:
		return rotate180(src)
	case 4:
		return flipV(src)
	case 5:
		return flipH(rotate90(src))
	case 6:
		return rotate90(src)
	case 7:
		return flipH(rotate270(src))
	case 8:
		return rotate270(src)
	default:
		return src
	}
}

func rotate90(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(h-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func rotate180(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(w-1-x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func rotate270(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, h, w))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func flipH(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(w-1-x, y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

func flipV(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst
}

// GenerateQRSVG returns a clean crisp SVG QR code for the given payload.
func GenerateQRSVG(content string) (string, error) {
	q, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bitmap := q.Bitmap()
	n := len(bitmap)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, n, n))
	sb.WriteString(fmt.Sprintf(`<rect width="%d" height="%d" fill="#ffffff" rx="2"/>`, n, n))
	sb.WriteString(`<path fill="#111114" d="`)
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			if bitmap[y][x] {
				sb.WriteString(fmt.Sprintf("M%d,%dh1v1h-1z", x, y))
			}
		}
	}
	sb.WriteString(`"/></svg>`)
	return sb.String(), nil
}
