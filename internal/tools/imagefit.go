package tools

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"sync"
)

const (
	maxImageEdge       = 2048
	maxImageDecodeSide = 4096
	maxImagePixels     = 8_000_000
	// maxDecodedBytes is the bitmap a decode is allowed to expand into.
	// 16-bit PNG uses more bytes per pixel than 8-bit.
	maxDecodedBytes = 8 << 20
)

// decodeMu keeps image.Decode to one at a time so two large bitmaps cannot
// land in memory together.
var decodeMu sync.Mutex

// fitImage keeps an image within the pixel cap. JPEG, PNG, and GIF that are
// larger than maxImageEdge on a side are downscaled. WebP has no decoder
// here: an over-size WebP is refused instead of being sent whole.
func fitImage(mime string, data []byte) (string, []byte, error) {
	if len(data) > maxImageSend {
		return "", nil, errors.New("view_image: image is larger than 10 MiB")
	}
	w, h, decoded, ok := imageMeta(mime, data)
	if !ok {
		return "", nil, errors.New("view_image: cannot read image dimensions")
	}
	if w > maxImageDecodeSide || h > maxImageDecodeSide || decoded > maxDecodedBytes || (w > 0 && h > maxImagePixels/w) {
		return "", nil, fmt.Errorf("view_image: image is %dx%d, over the pixel cap", w, h)
	}
	if mime == "image/webp" {
		if w > maxImageEdge || h > maxImageEdge {
			return "", nil, fmt.Errorf("view_image: webp is %dx%d, over %d px, and cannot be downscaled", w, h, maxImageEdge)
		}
		return mime, data, nil
	}
	if w <= maxImageEdge && h <= maxImageEdge {
		return mime, data, nil
	}
	img, err := decodeImage(data)
	if err != nil {
		return "", nil, fmt.Errorf("view_image: cannot downscale image: %w", err)
	}
	dst := downscale(img, maxImageEdge)
	var buf bytes.Buffer
	switch mime {
	case "image/jpeg":
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
			return "", nil, fmt.Errorf("view_image: cannot downscale image: %w", err)
		}
		if buf.Len() > maxImageSend {
			return "", nil, errors.New("view_image: image is larger than 10 MiB")
		}
		return "image/jpeg", buf.Bytes(), nil
	default:
		if err := png.Encode(&buf, dst); err != nil {
			return "", nil, fmt.Errorf("view_image: cannot downscale image: %w", err)
		}
		if buf.Len() > maxImageSend {
			return "", nil, errors.New("view_image: image is larger than 10 MiB")
		}
		return "image/png", buf.Bytes(), nil
	}
}

func decodeImage(data []byte) (image.Image, error) {
	decodeMu.Lock()
	defer decodeMu.Unlock()
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

func imageMeta(mime string, data []byte) (w, h int, decoded int64, ok bool) {
	w, h, ok = imageSize(mime, data)
	if !ok || w <= 0 || h <= 0 {
		return 0, 0, 0, false
	}
	bpp := int64(4)
	if mime == "image/png" {
		bpp = pngBytesPerPixel(data)
	}
	return w, h, int64(w) * int64(h) * bpp, true
}

// pngBytesPerPixel estimates the bitmap Go's decoder builds. 16-bit samples
// take two bytes each. 8-bit and below are counted as 4 bytes per pixel so a
// palette or gray image that expands to NRGBA is not under-counted.
func pngBytesPerPixel(b []byte) int64 {
	if len(b) < 26 {
		return 4
	}
	depth := int(b[24])
	samples := 4
	switch b[25] {
	case 0, 3:
		samples = 1
	case 2:
		samples = 3
	case 4:
		samples = 2
	case 6:
		samples = 4
	}
	if depth > 8 {
		return int64(samples * 2)
	}
	if samples < 4 {
		return 4
	}
	return int64(samples)
}

func downscale(src image.Image, edge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := scaledSize(w, h, edge)
	if nw == w && nh == h {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy := b.Min.Y + y*h/nh
		for x := 0; x < nw; x++ {
			sx := b.Min.X + x*w/nw
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func scaledSize(w, h, edge int) (int, int) {
	if w <= edge && h <= edge {
		return w, h
	}
	if w >= h {
		nh := h * edge / w
		if nh < 1 {
			nh = 1
		}
		return edge, nh
	}
	nw := w * edge / h
	if nw < 1 {
		nw = 1
	}
	return nw, edge
}

func imageSize(mime string, data []byte) (int, int, bool) {
	switch mime {
	case "image/png":
		return pngSize(data)
	case "image/jpeg":
		return jpegSize(data)
	case "image/gif":
		return gifSize(data)
	case "image/webp":
		return webpSize(data)
	default:
		return 0, 0, false
	}
}

func pngSize(b []byte) (int, int, bool) {
	if len(b) < 24 || string(b[12:16]) != "IHDR" {
		return 0, 0, false
	}
	w := int(binary.BigEndian.Uint32(b[16:20]))
	h := int(binary.BigEndian.Uint32(b[20:24]))
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

func gifSize(b []byte) (int, int, bool) {
	if len(b) < 10 {
		return 0, 0, false
	}
	w := int(binary.LittleEndian.Uint16(b[6:8]))
	h := int(binary.LittleEndian.Uint16(b[8:10]))
	if w <= 0 || h <= 0 {
		return 0, 0, false
	}
	return w, h, true
}

func jpegSize(b []byte) (int, int, bool) {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 0, 0, false
	}
	i := 2
	for i+4 < len(b) {
		if b[i] != 0xFF {
			return 0, 0, false
		}
		for i < len(b) && b[i] == 0xFF {
			i++
		}
		if i >= len(b) {
			return 0, 0, false
		}
		marker := b[i]
		i++
		if marker == 0xD8 || marker == 0xD9 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			continue
		}
		if i+1 >= len(b) {
			return 0, 0, false
		}
		seglen := int(b[i])<<8 | int(b[i+1])
		if seglen < 2 || i+seglen > len(b) {
			return 0, 0, false
		}
		if marker >= 0xC0 && marker <= 0xCF && marker != 0xC4 && marker != 0xC8 && marker != 0xCC {
			if seglen < 7 {
				return 0, 0, false
			}
			h := int(b[i+3])<<8 | int(b[i+4])
			w := int(b[i+5])<<8 | int(b[i+6])
			if w > 0 && h > 0 {
				return w, h, true
			}
			return 0, 0, false
		}
		if marker == 0xDA {
			return 0, 0, false
		}
		i += seglen
	}
	return 0, 0, false
}

func webpSize(b []byte) (int, int, bool) {
	if len(b) < 16 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return 0, 0, false
	}
	i := 12
	for i+8 <= len(b) {
		tag := string(b[i : i+4])
		size := int(binary.LittleEndian.Uint32(b[i+4 : i+8]))
		i += 8
		if size < 0 || i+size > len(b) {
			return 0, 0, false
		}
		switch tag {
		case "VP8X":
			if size >= 10 {
				w := 1 + int(b[i+4]) + int(b[i+5])<<8 + int(b[i+6])<<16
				h := 1 + int(b[i+7]) + int(b[i+8])<<8 + int(b[i+9])<<16
				if w > 0 && h > 0 {
					return w, h, true
				}
			}
		case "VP8 ":
			if size >= 10 && b[i+3] == 0x9D && b[i+4] == 0x01 && b[i+5] == 0x2A {
				w := int(binary.LittleEndian.Uint16(b[i+6:i+8])) & 0x3FFF
				h := int(binary.LittleEndian.Uint16(b[i+8:i+10])) & 0x3FFF
				if w > 0 && h > 0 {
					return w, h, true
				}
			}
		case "VP8L":
			if size >= 5 && b[i] == 0x2F {
				bits := uint32(b[i+1]) | uint32(b[i+2])<<8 | uint32(b[i+3])<<16 | uint32(b[i+4])<<24
				w := int(bits&0x3FFF) + 1
				h := int((bits>>14)&0x3FFF) + 1
				if w > 0 && h > 0 {
					return w, h, true
				}
			}
		}
		if size%2 == 1 {
			size++
		}
		i += size
	}
	return 0, 0, false
}

// Register gif's decoder. image.Decode needs it for the first frame.
var _ = gif.Decode
