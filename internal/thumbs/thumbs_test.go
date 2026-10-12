package thumbs

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// testImage builds an opaque gradient photo of the given size.
func testImage(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 256), uint8(y % 256), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func decodeDims(t *testing.T, data []byte) (int, int) {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("variant must decode: %v", err)
	}
	return img.Bounds().Dx(), img.Bounds().Dy()
}

// TestMakeScalesDown pins the long-edge contract: a 1600x900 photo at
// sm (384) becomes 384x216, at md (1280) becomes 1280x720.
func TestMakeScalesDown(t *testing.T) {
	src := testImage(1600, 900)

	sm, ok := Make(src, 384)
	if !ok {
		t.Fatal("sm variant must generate")
	}
	if w, h := decodeDims(t, sm); w != 384 || h != 216 {
		t.Errorf("sm = %dx%d, want 384x216", w, h)
	}

	md, ok := Make(src, 1280)
	if !ok {
		t.Fatal("md variant must generate")
	}
	if w, h := decodeDims(t, md); w != 1280 || h != 720 {
		t.Errorf("md = %dx%d, want 1280x720", w, h)
	}
}

// TestMakeOutputIsJPEG pins the serving contract: magic bytes + size
// smaller than a raw PNG photo of the same pixels.
func TestMakeOutputIsJPEG(t *testing.T) {
	src := testImage(1600, 900)
	sm, ok := Make(src, 384)
	if !ok {
		t.Fatal("sm variant must generate")
	}
	if len(sm) < 2 || sm[0] != 0xFF || sm[1] != 0xD8 {
		t.Error("variant must start with the JPEG magic bytes")
	}
	if len(sm) >= len(src) {
		t.Errorf("sm JPEG (%d) must be smaller than source PNG (%d)", len(sm), len(src))
	}
}

// TestMakeSmallPassthrough keeps tiny originals at native size (never
// upscaled), still re-encoded as JPEG for uniform serving.
func TestMakeSmallPassthrough(t *testing.T) {
	src := testImage(200, 100)
	out, ok := Make(src, 384)
	if !ok {
		t.Fatal("small variant must generate")
	}
	if w, h := decodeDims(t, out); w != 200 || h != 100 {
		t.Errorf("small image must keep native size, got %dx%d", w, h)
	}
}

// TestMakeRejectsNonImages proves graceful skip: PDFs and garbage must
// report ok=false so the caller serves the original instead.
func TestMakeRejectsNonImages(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":   {},
		"garbage": []byte("not an image at all"),
		"pdf":     []byte("%PDF-1.4 fake"),
	} {
		if _, ok := Make(data, 384); ok {
			t.Errorf("%s must not produce a variant", name)
		}
	}
}

// TestMakeFlattensAlpha proves transparent PNGs become viewable JPEGs
// (white, not black) instead of erroring.
func TestMakeFlattensAlpha(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 400, 400)) // fully transparent
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	out, ok := Make(buf.Bytes(), 384)
	if !ok {
		t.Fatal("transparent PNG must produce a variant")
	}
	dec, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("variant must be JPEG: %v", err)
	}
	r, g, b, _ := dec.At(10, 10).RGBA()
	if r < 0xf000 || g < 0xf000 || b < 0xf000 {
		t.Errorf("transparent pixel must flatten to white, got %04x %04x %04x", r, g, b)
	}
}

// TestVariantKey pins the deterministic storage layout and guards the
// traversal rules (suffix adds no "..", no leading slash).
func TestVariantKey(t *testing.T) {
	k := VariantKey("2026/10/some-uuid", SizeSm)
	if k != "2026/10/some-uuid.thumb-sm.jpg" {
		t.Errorf("unexpected variant key %q", k)
	}
	if !ValidSize(SizeSm) || !ValidSize(SizeMd) || ValidSize("xl") || ValidSize("../x") {
		t.Error("ValidSize must accept sm/md only")
	}
}
