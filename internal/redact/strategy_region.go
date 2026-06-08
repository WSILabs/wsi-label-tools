package redact

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"os"

	xdraw "golang.org/x/image/draw"

	"github.com/cornish/wsi-label-tools/internal/tifflow"
)

// parchment is the neutral fill for blanked label regions.
var parchment = color.RGBA{0xF5, 0xF5, 0xE6, 0xFF}

// hostInfo captures the host IFD's pixel layout for decode + re-encode.
type hostInfo struct {
	w, h, spp    int
	comp         uint64 // 1=uncompressed RGB, 7=JPEG
	tiled        bool
	tileW, tileH int
	rowsPerStrip int
	offs, cnts   []uint64
	jpegTables   []byte // tag 347 bytes, nil if absent (abbreviated JPEG)
}

// readHostInfo reads the host IFD's pixel layout: dims/comp/spp, tile-vs-strip
// geometry, the offsets/counts arrays (Tile* or Strip*), and the JPEGTables
// bytes (read out-of-line from the file, since they may exceed ReadIFD's
// in-memory cache threshold).
func readHostInfo(inPath string, file *tifflow.File, idx int) (*hostInfo, error) {
	if idx < 0 || idx >= len(file.IFDs) {
		return nil, fmt.Errorf("redact: host IFD index %d out of range", idx)
	}
	ifd := file.IFDs[idx]
	order := file.Header.ByteOrder

	hi := &hostInfo{}
	w, ok := ifd.UintValue(tifflow.TagImageWidth, order)
	if !ok {
		return nil, fmt.Errorf("redact: host IFD missing ImageWidth")
	}
	h, ok := ifd.UintValue(tifflow.TagImageLength, order)
	if !ok {
		return nil, fmt.Errorf("redact: host IFD missing ImageLength")
	}
	hi.w, hi.h = int(w), int(h)

	hi.spp = 3
	if spp, ok := ifd.UintValue(tifflow.TagSamplesPerPixel, order); ok && spp > 0 {
		hi.spp = int(spp)
	}

	hi.comp = 1
	if comp, ok := ifd.UintValue(tifflow.TagCompression, order); ok {
		hi.comp = comp
	}

	// Tile vs strip geometry.
	if tOffs, ok := ifd.UintArray(tifflow.TagTileOffsets, order); ok {
		hi.tiled = true
		hi.offs = tOffs
		cnts, ok2 := ifd.UintArray(tifflow.TagTileByteCounts, order)
		if !ok2 {
			return nil, fmt.Errorf("redact: host IFD missing TileByteCounts")
		}
		hi.cnts = cnts
		tw, _ := ifd.UintValue(tifflow.TagTileWidth, order)
		th, _ := ifd.UintValue(tifflow.TagTileLength, order)
		hi.tileW, hi.tileH = int(tw), int(th)
	} else {
		sOffs, ok := ifd.UintArray(tifflow.TagStripOffsets, order)
		if !ok {
			return nil, fmt.Errorf("redact: host IFD has neither tiles nor strips")
		}
		hi.offs = sOffs
		cnts, ok2 := ifd.UintArray(tifflow.TagStripByteCounts, order)
		if !ok2 {
			return nil, fmt.Errorf("redact: host IFD missing StripByteCounts")
		}
		hi.cnts = cnts
		rps := hi.h
		if v, ok := ifd.UintValue(tifflow.TagRowsPerStrip, order); ok && v > 0 {
			rps = int(v)
		}
		hi.rowsPerStrip = rps
	}

	// JPEGTables (tag 347): read out-of-line directly from the file, since the
	// blob may exceed ReadIFD's in-memory cache threshold.
	for _, e := range ifd.Entries {
		if e.Tag != tifflow.TagJPEGTables {
			continue
		}
		if e.DataSize == 0 {
			break
		}
		var tbl []byte
		if e.Inline {
			tbl = make([]byte, e.DataSize)
			copy(tbl, e.InlineValue)
		} else if len(e.InlineValue) == int(e.DataSize) {
			tbl = make([]byte, e.DataSize)
			copy(tbl, e.InlineValue)
		} else {
			f, err := os.Open(inPath)
			if err != nil {
				return nil, err
			}
			tbl = make([]byte, e.DataSize)
			_, err = f.ReadAt(tbl, int64(e.DataOffset))
			f.Close()
			if err != nil {
				return nil, fmt.Errorf("redact: read JPEGTables: %w", err)
			}
		}
		hi.jpegTables = tbl
		break
	}

	return hi, nil
}

// mergeAbbrev builds a self-contained JPEG from an abbreviated body and a
// JPEGTables blob: body[0:2] (SOI) + the tables segments between SOI and EOI
// + body[2:] (the rest of the abbreviated stream).
func mergeAbbrev(body, tables []byte) []byte {
	if len(tables) == 0 || len(body) < 2 {
		return body
	}
	// Strip SOI (FFD8) and trailing EOI (FFD9) from the tables stream so only
	// the table-defining segments remain.
	tbl := tables
	if len(tbl) >= 2 && tbl[0] == 0xFF && tbl[1] == 0xD8 {
		tbl = tbl[2:]
	}
	if len(tbl) >= 2 && tbl[len(tbl)-2] == 0xFF && tbl[len(tbl)-1] == 0xD9 {
		tbl = tbl[:len(tbl)-2]
	}
	out := make([]byte, 0, len(body)+len(tbl))
	out = append(out, body[0:2]...) // SOI
	out = append(out, tbl...)       // table segments
	out = append(out, body[2:]...)  // rest of abbreviated body
	return out
}

// readUnit reads strip/tile i raw bytes from the file.
func readUnit(f *os.File, hi *hostInfo, i int) ([]byte, error) {
	buf := make([]byte, hi.cnts[i])
	if _, err := f.ReadAt(buf, int64(hi.offs[i])); err != nil {
		return nil, fmt.Errorf("redact: read host unit %d: %w", i, err)
	}
	return buf, nil
}

// decodeJPEGUnit decodes a (possibly abbreviated) JPEG unit into an image.
func decodeJPEGUnit(raw []byte, hi *hostInfo) (image.Image, error) {
	if hi.jpegTables != nil {
		raw = mergeAbbrev(raw, hi.jpegTables)
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("redact: decode host JPEG: %w", err)
	}
	return img, nil
}

// decodeHost returns the host overview as a mutable *image.RGBA.
func decodeHost(inPath string, file *tifflow.File, hi *hostInfo) (*image.RGBA, error) {
	f, err := os.Open(inPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	switch hi.comp {
	case 1: // Uncompressed RGB strips.
		spp := hi.spp
		if spp <= 0 {
			spp = 3
		}
		stride := hi.w * spp
		raw := make([]byte, 0, stride*hi.h)
		for i := range hi.offs {
			buf, err := readUnit(f, hi, i)
			if err != nil {
				return nil, err
			}
			raw = append(raw, buf...)
		}
		if len(raw) < stride*hi.h {
			return nil, fmt.Errorf("redact: host uncompressed data short (%d < %d)", len(raw), stride*hi.h)
		}
		dst := image.NewRGBA(image.Rect(0, 0, hi.w, hi.h))
		for y := 0; y < hi.h; y++ {
			srcRow := raw[y*stride:]
			dstRow := dst.Pix[y*dst.Stride:]
			for x := 0; x < hi.w; x++ {
				s := srcRow[x*spp:]
				d := dstRow[x*4:]
				d[0] = s[0]
				if spp >= 3 {
					d[1] = s[1]
					d[2] = s[2]
				} else {
					d[1] = s[0]
					d[2] = s[0]
				}
				d[3] = 0xFF
			}
		}
		return dst, nil

	case 7: // JPEG.
		switch {
		case len(hi.offs) == 1:
			raw, err := readUnit(f, hi, 0)
			if err != nil {
				return nil, err
			}
			img, err := decodeJPEGUnit(raw, hi)
			if err != nil {
				return nil, err
			}
			return toRGBA(img), nil

		case hi.tiled && len(hi.offs) > 1:
			if hi.tileW <= 0 || hi.tileH <= 0 {
				return nil, fmt.Errorf("redact: host tiled overview missing tile dims")
			}
			across := (hi.w + hi.tileW - 1) / hi.tileW
			dst := image.NewRGBA(image.Rect(0, 0, hi.w, hi.h))
			for i := range hi.offs {
				if hi.cnts[i] == 0 {
					continue
				}
				raw, err := readUnit(f, hi, i)
				if err != nil {
					return nil, err
				}
				img, err := decodeJPEGUnit(raw, hi)
				if err != nil {
					return nil, err
				}
				x0 := (i % across) * hi.tileW
				y0 := (i / across) * hi.tileH
				r := image.Rect(x0, y0, x0+hi.tileW, y0+hi.tileH)
				draw.Draw(dst, r, img, img.Bounds().Min, draw.Src)
			}
			return dst, nil

		case !hi.tiled && len(hi.offs) > 1:
			if hi.rowsPerStrip <= 0 {
				return nil, fmt.Errorf("redact: host striped JPEG missing RowsPerStrip")
			}
			dst := image.NewRGBA(image.Rect(0, 0, hi.w, hi.h))
			for i := range hi.offs {
				if hi.cnts[i] == 0 {
					continue
				}
				raw, err := readUnit(f, hi, i)
				if err != nil {
					return nil, err
				}
				img, err := decodeJPEGUnit(raw, hi)
				if err != nil {
					return nil, err
				}
				y0 := i * hi.rowsPerStrip
				r := image.Rect(0, y0, hi.w, y0+img.Bounds().Dy())
				draw.Draw(dst, r, img, img.Bounds().Min, draw.Src)
			}
			return dst, nil

		default:
			return nil, fmt.Errorf("redact: unsupported host overview layout")
		}

	default:
		return nil, fmt.Errorf("redact: unsupported host overview layout")
	}
}

// encodeHost re-encodes img in the host's codec and splices it into outPath,
// preserving the host IFD's tags.
func encodeHost(inPath, outPath string, file *tifflow.File, hi *hostInfo, idx int, img *image.RGBA, fsync bool) error {
	switch hi.comp {
	case 1: // Uncompressed RGB: re-split into the SAME strip layout.
		spp := hi.spp
		if spp <= 0 {
			spp = 3
		}
		if hi.rowsPerStrip <= 0 {
			return fmt.Errorf("redact: host uncompressed overview missing RowsPerStrip")
		}
		strips := make([][]byte, len(hi.offs))
		for i := range hi.offs {
			y0 := i * hi.rowsPerStrip
			y1 := y0 + hi.rowsPerStrip
			if y1 > hi.h {
				y1 = hi.h
			}
			if y0 >= hi.h {
				strips[i] = []byte{}
				continue
			}
			rows := y1 - y0
			buf := make([]byte, rows*hi.w*spp)
			p := 0
			for y := y0; y < y1; y++ {
				dstRow := img.Pix[y*img.Stride:]
				for x := 0; x < hi.w; x++ {
					d := dstRow[x*4:]
					buf[p] = d[0]
					if spp >= 3 {
						buf[p+1] = d[1]
						buf[p+2] = d[2]
					}
					p += spp
				}
			}
			strips[i] = buf
		}
		return tifflow.Splice(tifflow.SpliceParams{
			InPath: inPath, OutPath: outPath, File: file,
			Mode: tifflow.SpliceReplaceStripData, TargetIdx: idx,
			StripData: strips, Fsync: fsync,
		})

	case 7: // JPEG: only single-unit supported.
		if len(hi.offs) != 1 {
			return fmt.Errorf("redact: unsupported multi-unit JPEG overview")
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return fmt.Errorf("redact: encode host: %w", err)
		}
		var dropTags []uint16
		if hi.jpegTables != nil {
			dropTags = []uint16{tifflow.TagJPEGTables}
		}
		return tifflow.Splice(tifflow.SpliceParams{
			InPath: inPath, OutPath: outPath, File: file,
			Mode: tifflow.SpliceReplaceStripData, TargetIdx: idx,
			StripData: [][]byte{buf.Bytes()}, DropTags: dropTags, Fsync: fsync,
		})

	default:
		return fmt.Errorf("redact: unsupported host overview layout")
	}
}

// toRGBA returns a mutable RGBA copy of img with origin at (0,0).
func toRGBA(img image.Image) *image.RGBA {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

// regionStrip blanks the target region of the host image with a neutral fill.
func regionStrip(inPath, outPath string, file *tifflow.File, t *Target, fsync bool) error {
	hi, err := readHostInfo(inPath, file, t.HostIFDIndex)
	if err != nil {
		return err
	}
	dst, err := decodeHost(inPath, file, hi)
	if err != nil {
		return err
	}
	region := t.Region.Add(dst.Bounds().Min)
	draw.Draw(dst, region, image.NewUniform(parchment), image.Point{}, draw.Src)
	return encodeHost(inPath, outPath, file, hi, t.HostIFDIndex, dst, fsync)
}

// regionReplace composites the supplied label image into the target region,
// scaling it to fill the region rectangle exactly. NOTE: for EmbeddedRegion
// targets the geometry knobs (Rotate, Resize, BG, Force) are no-ops — the
// image is stretched to the region's fixed footprint within the host image,
// not letterboxed/rotated. Those options apply only to the Splice path.
func regionReplace(inPath, outPath string, file *tifflow.File, c Classifier, t *Target, opts ReplaceOptions) (*Result, error) {
	hi, err := readHostInfo(inPath, file, t.HostIFDIndex)
	if err != nil {
		return nil, err
	}
	dst, err := decodeHost(inPath, file, hi)
	if err != nil {
		return nil, err
	}
	region := t.Region.Add(dst.Bounds().Min)
	xdraw.CatmullRom.Scale(dst, region, opts.Image, opts.Image.Bounds(), xdraw.Over, nil)
	if err := encodeHost(inPath, outPath, file, hi, t.HostIFDIndex, dst, opts.Fsync); err != nil {
		return nil, err
	}
	return &Result{}, nil
}
