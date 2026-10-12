// Package thumbs generates small JPEG display variants of catalog
// images. Originals stay untouched in storage; variants live beside
// them under deterministic keys (see VariantKey) so no database
// column is needed. Anything that cannot be decoded is skipped by
// the caller — a missing variant always falls back to the original.
package thumbs

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"

	// Register decoders: JPEG/PNG/GIF come from the stdlib, WebP from
	// x/image. The upload whitelist only admits these image types.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	stdraw "image/draw"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// Mime is the content type of every generated variant (always JPEG:
// predictable for serving, no alpha, small).
const Mime = "image/jpeg"

// Quality balances sample-photo clarity against mobile page weight.
const Quality = 85

// Size names served by the public catalog (?size=sm|md).
const (
	SizeSm = "sm"
	SizeMd = "md"
)

// MaxEdge caps the long edge per size: sm feeds the 96px dialog
// thumbnails (with headroom for dense screens), md feeds the
// full-width cover cards.
func MaxEdge(size string) (int, bool) {
	switch size {
	case SizeSm:
		return 384, true
	case SizeMd:
		return 1280, true
	default:
		return 0, false
	}
}

// ValidSize reports whether size is a servable variant name.
func ValidSize(size string) bool {
	_, ok := MaxEdge(size)
	return ok
}

// VariantKey derives the storage key of a variant from the original
// key: "<key>.thumb-<size>.jpg". The suffix cannot introduce ".." or
// an absolute path, so every valid original key yields a valid key.
func VariantKey(key, size string) string {
	return fmt.Sprintf("%s.thumb-%s.jpg", key, size)
}

// Make decodes data and returns a JPEG variant whose long edge fits
// maxEdge. Smaller images are re-encoded as-is (uniform output, one
// code path on serve). ok=false means "not decodable": the caller
// must skip variant storage and serve the original.
func Make(data []byte, maxEdge int) (out []byte, ok bool) {
	img, err := decode(data)
	if err != nil {
		return nil, false
	}
	if maxEdge > 0 {
		img = fit(img, maxEdge)
	}
	img = flatten(img)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: Quality}); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// decode sniffs jpeg/png/gif/webp. PDFs and anything else fail here
// and are skipped by the caller.
func decode(data []byte) (image.Image, error) {
	if img, _, err := image.Decode(bytes.NewReader(data)); err == nil {
		return img, nil
	}
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return img, nil
}

// fit scales img down so its long edge is at most maxEdge, preserving
// aspect ratio. Smaller images pass through untouched.
func fit(img image.Image, maxEdge int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	long := w
	if h > long {
		long = h
	}
	if long <= maxEdge || long <= 0 {
		return img
	}
	var nw, nh int
	if w >= h {
		nw, nh = maxEdge, h*maxEdge/w
	} else {
		nw, nh = w*maxEdge/h, maxEdge
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, stdraw.Over, nil)
	return dst
}

// flatten composites img onto an opaque white canvas. JPEG has no
// alpha channel; without this, transparent PNGs encode as black.
func flatten(img image.Image) image.Image {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	stdraw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, stdraw.Src)
	stdraw.Draw(dst, dst.Bounds(), img, b.Min, stdraw.Over)
	return dst
}
