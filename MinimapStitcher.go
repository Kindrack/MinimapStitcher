package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const atlasCols = 4
const atlasRows = 3

var (
	coordNameRE       = regexp.MustCompile(`^(.+?)(?:_)?([0-9]+)_([0-9]+)$`)
	atlasUnderscoreRE = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*?)([0-9]+)_([0-9]+)$`)
	atlasNamedRE      = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*?)_([0-9]+)$`)
	atlasPlainRE      = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]*?)([0-9]+)$`)
	hashRE            = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
)

type Tile struct {
	X, Y int
	Path string
}
type Atlas struct {
	Name  string
	Files map[int]string
}
type Job struct {
	Name     string
	Tiles    []Tile
	Atlas    *Atlas
	NoLiquid bool
}

type Translator struct {
	paths     map[string]string
	ambiguous map[string]bool
}

type Stats struct {
	FilesFound      int
	FilesIgnored    int
	MapsStitched    int
	MapsSkipped     int
	DecodeErrors    int
	TilesMissing    int
	HashesResolved  int
	HashesUnknown   int
	HashesAmbiguous int
}

func main() {
	fmt.Println("==========================================")
	fmt.Println("   World of Warcraft - Minimap Stitcher   ")
	fmt.Println("==========================================")
	source := ""
	dest := "./StitchedMaps"
	if len(os.Args) > 1 {
		source = os.Args[1]
	}
	if len(os.Args) > 2 {
		dest = os.Args[2]
	}
	if source == "" {
		source = findMinimapFolder(".")
	}
	if source == "" {
		fmt.Println("Error: no Minimap or Minimaps folder was found.")
		fmt.Println("Place a Minimap or Minimaps folder next to the program, or pass the source folder as the first argument.")
		fmt.Println("Press Enter to close this window...")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		os.Exit(1)
	}
	fmt.Printf("Using source folder: %s\n", source)
	if err := run(source, dest); err != nil {
		fmt.Printf("Error: %v\n", err)
		fmt.Println("Press Enter to close this window...")
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		os.Exit(1)
	}
	fmt.Println("Press Enter to close this window...")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
}

func findMinimapFolder(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	// Prefer "Minimaps" when both names exist, but accept either spelling.
	for _, preferred := range []string{"Minimaps", "Minimap"} {
		for _, entry := range entries {
			if entry.IsDir() && strings.EqualFold(entry.Name(), preferred) {
				return filepath.Join(root, entry.Name())
			}
		}
	}
	return ""
}

func run(source, dest string) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	tr := loadTranslators(source)
	files, err := collectFiles(source)
	if err != nil {
		return err
	}

	jobs, skipped, hashResolved, hashUnknown, hashAmbiguous := buildJobs(files, tr)
	stats := Stats{
		FilesFound:      len(files),
		FilesIgnored:    skipped,
		HashesResolved:  hashResolved,
		HashesUnknown:   hashUnknown,
		HashesAmbiguous: hashAmbiguous,
	}
	if len(jobs) == 0 {
		return errors.New("no stitchable minimap tiles were found")
	}

	sort.Slice(jobs, func(i, j int) bool { return jobs[i].Name < jobs[j].Name })
	for _, j := range jobs {
		var result stitchResult
		if j.Atlas != nil {
			result = stitchAtlas(j, dest)
		} else if len(j.Tiles) > 0 {
			result = stitchCoordinates(j, dest)
		}
		if result.err != nil {
			fmt.Printf("[%s] skipped: %v\n", j.Name, result.err)
			stats.MapsSkipped++
			stats.DecodeErrors += result.decodeErrors
			stats.TilesMissing += result.missingTiles
			continue
		}
		stats.MapsStitched++
		stats.DecodeErrors += result.decodeErrors
		stats.TilesMissing += result.missingTiles
		if result.decodeErrors > 0 || result.missingTiles > 0 {
			fmt.Printf("[%s] stitched with %d decode error(s), %d missing tile position(s).\n", j.Name, result.decodeErrors, result.missingTiles)
		}
	}

	fmt.Printf("\nFinished:\n")
	fmt.Printf("  Maps stitched:   %d\n", stats.MapsStitched)
	fmt.Printf("  Maps skipped:    %d\n", stats.MapsSkipped)
	fmt.Printf("  Files found:     %d\n", stats.FilesFound)
	fmt.Printf("  Files ignored:   %d\n", stats.FilesIgnored)
	fmt.Printf("  Decode errors:   %d\n", stats.DecodeErrors)
	fmt.Printf("  Missing tiles:   %d\n", stats.TilesMissing)
	if stats.HashesResolved+stats.HashesUnknown+stats.HashesAmbiguous > 0 {
		fmt.Printf("  Hashes resolved:  %d\n", stats.HashesResolved)
		fmt.Printf("  Hashes unknown:   %d\n", stats.HashesUnknown)
		fmt.Printf("  Hashes ambiguous: %d\n", stats.HashesAmbiguous)
	}
	return nil
}

func collectFiles(root string) ([]string, error) {
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.EqualFold(info.Name(), "WMO") {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".png" || ext == ".blp" || strings.EqualFold(info.Name(), "md5translate.trs") {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}

func loadTranslators(root string) Translator {
	t := Translator{paths: map[string]string{}, ambiguous: map[string]bool{}}
	var trs []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && strings.EqualFold(info.Name(), "md5translate.trs") {
			trs = append(trs, path)
		}
		return nil
	})
	for _, p := range trs {
		parseTRS(p, &t)
	}
	if len(t.paths) > 0 {
		fmt.Printf("Loaded %d hash translation(s) from %d md5translate.trs file(s).\n", len(t.paths), len(trs))
	}
	return t
}

func parseTRS(path string, t *Translator) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(s.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		a, b := strings.Trim(fields[0], "\"'"), strings.Trim(fields[len(fields)-1], "\"'")
		var logical, hash string
		if isHashName(a) {
			hash, logical = normalizeHash(a), b
		} else if isHashName(b) {
			hash, logical = normalizeHash(b), a
		} else {
			continue
		}
		logical = strings.TrimSpace(strings.Trim(logical, "\"'"))
		if logical == "" {
			continue
		}
		if old, ok := t.paths[hash]; ok && !strings.EqualFold(old, logical) {
			t.ambiguous[hash] = true
		} else if !t.ambiguous[hash] {
			t.paths[hash] = logical
		}
	}
}

func isHashName(s string) bool {
	s = strings.TrimSuffix(strings.TrimSuffix(filepath.Base(strings.ReplaceAll(s, "\\", "/")), ".blp"), ".png")
	return hashRE.MatchString(s)
}
func normalizeHash(s string) string {
	b := filepath.Base(strings.ReplaceAll(strings.TrimSpace(s), "\\", "/"))
	lower := strings.ToLower(b)
	if strings.HasSuffix(lower, ".blp") || strings.HasSuffix(lower, ".png") {
		b = b[:len(b)-4]
	}
	return strings.ToLower(b)
}

func buildJobs(files []string, tr Translator) ([]Job, int, int, int, int) {
	// key -> coordinates; separate noLiquid variants.
	coord := map[string]map[string]Tile{}
	atlas := map[string]map[int]string{}
	ignored := 0
	hashResolved := 0
	hashUnknown := 0
	hashAmbiguous := 0

	addCoord := func(mapName string, x, y int, path string, noLiquid bool) {
		key := mapName
		if noLiquid {
			key += "NoLiquid"
		}
		if coord[key] == nil {
			coord[key] = map[string]Tile{}
		}
		k := fmt.Sprintf("%d_%d", x, y)
		if _, exists := coord[key][k]; !exists {
			coord[key][k] = Tile{X: x, Y: y, Path: path}
		}
	}
	addAtlas := func(name string, idx int, path string) {
		if atlas[name] == nil {
			atlas[name] = map[int]string{}
		}
		if _, exists := atlas[name][idx]; !exists {
			atlas[name][idx] = path
		}
	}

	for _, path := range files {
		base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		ext := strings.ToLower(filepath.Ext(path))
		if strings.EqualFold(filepath.Base(path), "md5translate.trs") {
			continue
		}
		lowerBase := strings.ToLower(base)
		noLiquid := strings.Contains(lowerBase, "noliquid")

		// Resolve 32-character hash names using md5translate.trs. The .trs extension is deliberately ignored.
		if hashRE.MatchString(base) {
			h := strings.ToLower(base)
			if tr.ambiguous[h] {
				hashAmbiguous++
				ignored++
				continue
			}
			logical, ok := tr.paths[h]
			if !ok {
				hashUnknown++
				ignored++
				continue
			}
			mapName, x, y, ok := parseLogicalPath(logical)
			if ok {
				addCoord(mapName, x, y, path, false)
				hashResolved++
				continue
			}
			hashUnknown++
			ignored++
			continue
		}

		// Existing 12-tile atlas conventions. Both 1-based (1..12) and
		// 0-based (0..11) numbering are accepted. Keep the source index
		// unchanged here; the complete set is normalized below.
		if m := atlasUnderscoreRE.FindStringSubmatch(base); m != nil {
			idx, _ := strconv.Atoi(m[3])
			if idx >= 0 && idx <= 12 {
				addAtlas(m[1]+m[2], idx, path)
				continue
			}
		}
		if m := atlasNamedRE.FindStringSubmatch(base); m != nil {
			idx, _ := strconv.Atoi(m[2])
			if idx >= 0 && idx <= 12 {
				addAtlas(m[1], idx, path)
				continue
			}
		}
		if m := atlasPlainRE.FindStringSubmatch(base); m != nil {
			idx, _ := strconv.Atoi(m[2])
			if idx >= 0 && idx <= 12 {
				addAtlas(m[1], idx, path)
				continue
			}
		}

		// Direct coordinate files such as Azeroth_24_53.png or map24_53.blp.
		if mapName, x, y, ok := parseCoordinateBase(base, filepath.Dir(path)); ok {
			addCoord(mapName, x, y, path, noLiquid)
			continue
		}
		_ = ext
		ignored++
	}

	var jobs []Job
	// Exact 12-tile atlas sets are separate outputs. Normalize either
	// 0..11 or 1..12 numbering to the internal 1..12 representation.
	for name, fs := range atlas {
		if len(fs) != 12 {
			ignored += len(fs)
			continue
		}

		normalized := make(map[int]string, 12)
		zeroBased := fs[0] != ""
		good := true
		if zeroBased {
			for i := 0; i <= 11; i++ {
				if fs[i] == "" {
					good = false
					break
				}
				normalized[i+1] = fs[i]
			}
		} else {
			for i := 1; i <= 12; i++ {
				if fs[i] == "" {
					good = false
					break
				}
				normalized[i] = fs[i]
			}
		}
		if !good {
			ignored += len(fs)
			continue
		}
		jobs = append(jobs, Job{Name: name, Atlas: &Atlas{Name: name, Files: normalized}})
	}
	for name, ts := range coord {
		if len(ts) == 0 {
			continue
		}
		arr := make([]Tile, 0, len(ts))
		for _, t := range ts {
			arr = append(arr, t)
		}
		sort.Slice(arr, func(i, j int) bool {
			if arr[i].X == arr[j].X {
				return arr[i].Y < arr[j].Y
			}
			return arr[i].X < arr[j].X
		})
		jobs = append(jobs, Job{Name: name, Tiles: arr, NoLiquid: strings.HasSuffix(name, "NoLiquid")})
	}
	return jobs, ignored, hashResolved, hashUnknown, hashAmbiguous
}

func parseLogicalPath(logical string) (string, int, int, bool) {
	p := strings.ReplaceAll(logical, "\\", "/")
	p = strings.Trim(p, "/")
	partsRaw := strings.Split(p, "/")
	parts := partsRaw[:0]
	for _, part := range partsRaw {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "", 0, 0, false
	}
	base := strings.TrimSuffix(strings.TrimSuffix(parts[len(parts)-1], ".blp"), ".png")
	mapName := ""
	if len(parts) > 1 {
		mapName = parts[len(parts)-2]
	}
	if mapName == "" {
		mapName = strings.TrimPrefix(base, "map")
	}
	m := regexp.MustCompile(`^map([0-9]+)_([0-9]+)$`).FindStringSubmatch(base)
	if m == nil {
		m = regexp.MustCompile(`^([0-9]+)_([0-9]+)$`).FindStringSubmatch(base)
	}
	if m == nil {
		return "", 0, 0, false
	}
	x, _ := strconv.Atoi(m[1])
	y, _ := strconv.Atoi(m[2])
	return mapName, x, y, true
}

func parseCoordinateBase(base, dir string) (string, int, int, bool) {
	// First support WoW's usual mapX_Y naming, using the containing directory as map name.
	if m := regexp.MustCompile(`^map([0-9]+)_([0-9]+)$`).FindStringSubmatch(base); m != nil {
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		mapName := filepath.Base(dir)
		if mapName == "." || mapName == "" || strings.EqualFold(mapName, "Minimaps") {
			mapName = "WorldMap"
		}
		return mapName, x, y, true
	}
	if m := coordNameRE.FindStringSubmatch(base); m != nil {
		// Avoid interpreting a plain atlas file as a coordinate file.
		prefix := m[1]
		x, _ := strconv.Atoi(m[2])
		y, _ := strconv.Atoi(m[3])
		return strings.TrimSuffix(prefix, "_"), x, y, true
	}
	return "", 0, 0, false
}

type stitchResult struct {
	err          error
	decodeErrors int
	missingTiles int
}

func stitchCoordinates(j Job, dest string) stitchResult {
	if len(j.Tiles) == 0 {
		return stitchResult{err: errors.New("no tiles")}
	}
	minX, maxX, minY, maxY := j.Tiles[0].X, j.Tiles[0].X, j.Tiles[0].Y, j.Tiles[0].Y
	for _, t := range j.Tiles {
		if t.X < minX {
			minX = t.X
		}
		if t.X > maxX {
			maxX = t.X
		}
		if t.Y < minY {
			minY = t.Y
		}
		if t.Y > maxY {
			maxY = t.Y
		}
	}
	tileW, tileH, err := imageSize(j.Tiles[0].Path)
	if err != nil {
		return stitchResult{err: err, decodeErrors: 1}
	}
	if tileW == 0 || tileH == 0 {
		return stitchResult{err: errors.New("invalid tile size")}
	}
	out := image.NewRGBA(image.Rect(0, 0, (maxX-minX+1)*tileW, (maxY-minY+1)*tileH))
	present := make(map[string]bool, len(j.Tiles))
	decodeErrors := 0
	for _, t := range j.Tiles {
		img, err := decodeImage(t.Path)
		if err != nil {
			decodeErrors++
			fmt.Printf("[%s] tile %s: %v\n", j.Name, filepath.Base(t.Path), err)
			continue
		}
		if img.Bounds().Dx() != tileW || img.Bounds().Dy() != tileH {
			decodeErrors++
			fmt.Printf("[%s] tile %s: mixed tile size (%dx%d, expected %dx%d)\n", j.Name, filepath.Base(t.Path), img.Bounds().Dx(), img.Bounds().Dy(), tileW, tileH)
			continue
		}
		x := (t.X - minX) * tileW
		y := (t.Y - minY) * tileH
		draw.Draw(out, image.Rect(x, y, x+tileW, y+tileH), img, img.Bounds().Min, draw.Over)
		present[fmt.Sprintf("%d_%d", t.X, t.Y)] = true
	}
	missing := 0
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			if !present[fmt.Sprintf("%d_%d", x, y)] {
				missing++
			}
		}
	}
	if len(present) == 0 {
		return stitchResult{err: errors.New("all tiles failed to decode"), decodeErrors: decodeErrors, missingTiles: missing}
	}
	if err := writePNG(filepath.Join(dest, j.Name+".png"), out); err != nil {
		return stitchResult{err: err, decodeErrors: decodeErrors, missingTiles: missing}
	}
	fmt.Printf("[%s] tile size: %dx%d, grid: %dx%d, output: %dx%d\n", j.Name, tileW, tileH, maxX-minX+1, maxY-minY+1, out.Bounds().Dx(), out.Bounds().Dy())
	return stitchResult{decodeErrors: decodeErrors, missingTiles: missing}
}

func stitchAtlas(j Job, dest string) stitchResult {
	first, err := decodeImage(j.Atlas.Files[1])
	if err != nil {
		return stitchResult{err: err, decodeErrors: 1}
	}
	w, h := first.Bounds().Dx(), first.Bounds().Dy()
	if w != h {
		return stitchResult{err: errors.New("atlas tiles are not square")}
	}
	out := image.NewRGBA(image.Rect(0, 0, atlasCols*w, atlasRows*h))
	decodeErrors := 0
	for i := 1; i <= 12; i++ {
		img, err := decodeImage(j.Atlas.Files[i])
		if err != nil {
			decodeErrors++
			fmt.Printf("[%s] tile %d (%s): %v\n", j.Name, i, filepath.Base(j.Atlas.Files[i]), err)
			continue
		}
		if img.Bounds().Dx() != w || img.Bounds().Dy() != h {
			decodeErrors++
			fmt.Printf("[%s] tile %d: mixed atlas tile size\n", j.Name, i)
			continue
		}
		x := ((i - 1) % atlasCols) * w
		y := ((i - 1) / atlasCols) * h
		draw.Draw(out, image.Rect(x, y, x+w, y+h), img, img.Bounds().Min, draw.Over)
	}
	if err := writePNG(filepath.Join(dest, j.Name+".png"), out); err != nil {
		return stitchResult{err: err, decodeErrors: decodeErrors}
	}
	fmt.Printf("[%s] atlas tile size: %dx%d, output: %dx%d\n", j.Name, w, h, out.Bounds().Dx(), out.Bounds().Dy())
	return stitchResult{decodeErrors: decodeErrors}
}

func imageSize(path string) (int, int, error) {
	img, err := decodeImage(path)
	if err != nil {
		return 0, 0, err
	}
	return img.Bounds().Dx(), img.Bounds().Dy(), nil
}
func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func decodeImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".png" {
		return png.Decode(f)
	}
	if ext == ".blp" {
		return decodeBLP(f)
	}
	return nil, fmt.Errorf("unsupported image type %s", ext)
}

func decodeBLP(r io.Reader) (image.Image, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if len(b) < 156 || string(b[:4]) != "BLP2" {
		return nil, errors.New("unsupported BLP format (expected BLP2)")
	}
	typ := binary.LittleEndian.Uint32(b[4:8])
	if typ != 1 {
		return nil, errors.New("JPEG BLP is not supported")
	}
	compression := b[8]
	alphaDepth := b[9]
	alphaType := b[10]
	width := int(binary.LittleEndian.Uint32(b[12:16]))
	height := int(binary.LittleEndian.Uint32(b[16:20]))
	if width <= 0 || height <= 0 || width > 32768 || height > 32768 {
		return nil, errors.New("invalid BLP dimensions")
	}
	offsets := make([]uint32, 16)
	lengths := make([]uint32, 16)
	for i := 0; i < 16; i++ {
		offsets[i] = binary.LittleEndian.Uint32(b[20+i*4 : 24+i*4])
		lengths[i] = binary.LittleEndian.Uint32(b[84+i*4 : 88+i*4])
	}
	if offsets[0] == 0 || int(offsets[0]) >= len(b) {
		return nil, errors.New("invalid BLP mipmap offset")
	}
	pal := make([]color.RGBA, 256)
	for i := 0; i < 256; i++ {
		p := 156 + i*4
		pal[i] = color.RGBA{R: b[p+2], G: b[p+1], B: b[p], A: b[p+3]}
	}
	end := len(b)
	if lengths[0] > 0 && int(offsets[0])+int(lengths[0]) < end {
		end = int(offsets[0]) + int(lengths[0])
	}
	data := b[int(offsets[0]):end]
	switch compression {
	case 1:
		return decodePaletted(data, width, height, alphaDepth, pal)
	case 2:
		return decodeDXT(data, width, height, alphaDepth, alphaType)
	case 3:
		return decodeBGRA(data, width, height)
	default:
		return nil, fmt.Errorf("unsupported BLP compression %d", compression)
	}
}

func decodePaletted(data []byte, w, h int, depth byte, pal []color.RGBA) (image.Image, error) {
	pixels := w * h
	if len(data) < pixels {
		return nil, errors.New("truncated BLP palette data")
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	idx := data[:pixels]
	alpha := data[pixels:]
	for i, v := range idx {
		c := pal[int(v)]
		c.A = 255
		if depth == 8 {
			if i < len(alpha) {
				c.A = alpha[i]
			}
		} else if depth == 1 {
			if i/8 < len(alpha) && ((alpha[i/8]>>(uint(i)&7))&1) == 0 {
				c.A = 0
			}
		} else if depth == 4 {
			if i/2 < len(alpha) {
				a := alpha[i/2]
				if i%2 == 0 {
					c.A = (a & 15) * 17
				} else {
					c.A = (a >> 4) * 17
				}
			}
		}
		out.SetRGBA(i%w, i/w, c)
	}
	return out, nil
}

func decodeBGRA(data []byte, w, h int) (image.Image, error) {
	need := w * h * 4
	if len(data) < need {
		return nil, errors.New("truncated BLP BGRA data")
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		p := i * 4
		out.SetRGBA(i%w, i/w, color.RGBA{R: data[p+2], G: data[p+1], B: data[p], A: data[p+3]})
	}
	return out, nil
}

func decodeDXT(data []byte, w, h int, depth, alphaType byte) (image.Image, error) {
	blockBytes := 8
	if alphaType == 1 || alphaType == 7 || alphaType == 8 {
		blockBytes = 16
	}
	bw := (w + 3) / 4
	bh := (h + 3) / 4
	need := bw * bh * blockBytes
	if len(data) < need {
		return nil, fmt.Errorf("truncated DXT data: have %d need %d", len(data), need)
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	p := 0
	for by := 0; by < bh; by++ {
		for bx := 0; bx < bw; bx++ {
			var alphas [16]uint8
			var hasAlpha bool
			if blockBytes == 16 {
				if alphaType == 1 {
					for i := 0; i < 8; i++ {
						v := data[p+i]
						alphas[i*2] = (v & 15) * 17
						alphas[i*2+1] = (v >> 4) * 17
					}
					hasAlpha = true
					p += 8
				} else {
					a0, a1 := data[p], data[p+1]
					var tab [8]uint8
					tab[0] = a0
					tab[1] = a1
					if a0 > a1 {
						for i := 2; i < 8; i++ {
							tab[i] = uint8(((8-i)*int(a0) + (i-1)*int(a1)) / 7)
						}
					} else {
						for i := 2; i < 6; i++ {
							tab[i] = uint8(((6-i)*int(a0) + (i-1)*int(a1)) / 5)
						}
						tab[6] = 0
						tab[7] = 255
					}
					for i := 0; i < 16; i++ {
						bit := uint(i * 3)
						code := (uint64(data[p+2]) | uint64(data[p+3])<<8 | uint64(data[p+4])<<16 | uint64(data[p+5])<<24 | uint64(data[p+6])<<32 | uint64(data[p+7])<<40) >> bit
						code &= 7
						alphas[i] = tab[code]
					}
					hasAlpha = true
					p += 8
				}
			}
			c0 := binary.LittleEndian.Uint16(data[p : p+2])
			c1 := binary.LittleEndian.Uint16(data[p+2 : p+4])
			var cols [4]color.RGBA
			cols[0] = rgb565(c0, 255)
			cols[1] = rgb565(c1, 255)
			if c0 > c1 || alphaType == 1 || alphaType == 7 || alphaType == 8 {
				cols[2] = interp(cols[0], cols[1], 2, 1, 3)
				cols[3] = interp(cols[0], cols[1], 1, 2, 3)
			} else {
				cols[2] = interp(cols[0], cols[1], 1, 1, 2)
				cols[3] = color.RGBA{}
			}
			bits := binary.LittleEndian.Uint32(data[p+4 : p+8])
			p += 8
			for py := 0; py < 4; py++ {
				for px := 0; px < 4; px++ {
					i := py*4 + px
					ci := uint8((bits >> uint(i*2)) & 3)
					c := cols[ci]
					if ci == 3 && c.A == 0 && c.R == 0 && c.G == 0 && c.B == 0 && c0 <= c1 && alphaType != 1 && alphaType != 7 && alphaType != 8 {
						c.A = 0
					}
					if hasAlpha {
						c.A = alphas[i]
					}
					x := bx*4 + px
					y := by*4 + py
					if x < w && y < h {
						out.SetRGBA(x, y, c)
					}
				}
			}
		}
	}
	_ = depth
	return out, nil
}
func rgb565(v uint16, a uint8) color.RGBA {
	return color.RGBA{R: uint8(((v >> 11) & 31) * 255 / 31), G: uint8(((v >> 5) & 63) * 255 / 63), B: uint8((v & 31) * 255 / 31), A: a}
}
func interp(a, b color.RGBA, wa, wb, den int) color.RGBA {
	return color.RGBA{R: uint8((wa*int(a.R) + wb*int(b.R)) / den), G: uint8((wa*int(a.G) + wb*int(b.G)) / den), B: uint8((wa*int(a.B) + wb*int(b.B)) / den), A: 255}
}
