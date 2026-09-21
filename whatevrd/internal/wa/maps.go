package wa

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	// Registered for its decoder: the standard OSM layer serves PNG, and some
	// mirrors serve JPEG.
	_ "image/jpeg"
)

// A location bubble needs a map, and the embedded JPEG WhatsApp ships with a
// LocationMessage is about 200px wide: fine on a phone, soft and small in a
// desktop bubble. So the daemon fetches raster tiles and stitches its own.
//
// This is deliberately daemon-side. Only one process talks to the tile server,
// tiles are cached by z/x/y and shared across every message that lands near
// them, and a frontend never makes a network request of its own. It also means
// a stitched map is an ordinary file in the media cache, so it rides the
// existing download lifecycle (downloading -> path -> download_error) and gets
// the progress ring and the retry button for free.

const (
	// mapTileSize is the edge of a slippy-map tile in pixels. 256 is the
	// universal raster convention and what the OSM tile servers serve.
	mapTileSize = 256
	// mapZoom is close enough to read street names without pulling a wide area
	// of tiles for a single pin.
	mapZoom = 16
	// The stitched map is 3x2 tiles, cropped to the bubble's aspect. Wider than
	// tall, because that is the shape a card wants and it keeps a pin centred
	// with useful context either side.
	mapTilesX = 3
	mapTilesY = 2
	// mapOutputWidth and mapOutputHeight crop the stitched grid around the pin.
	// A 3x2 grid is 768x512; cropping to 640x400 keeps the pin centred with the
	// tile seams safely outside the frame.
	mapOutputWidth  = 640
	mapOutputHeight = 400

	// DefaultMapTileURLTemplate is the OSM standard raster layer. Overridable
	// through daemon config for anyone who would rather point elsewhere.
	DefaultMapTileURLTemplate = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"

	// mapTileFetchTimeout bounds one tile. Six of them run concurrently, so a
	// whole map is at most this long, not six times it.
	mapTileFetchTimeout = 15 * time.Second
	// mapTileMaxBytes rejects a tile server handing back something that is not
	// a tile. A 256px PNG tile is a few tens of KB.
	mapTileMaxBytes = 2 << 20
)

// ErrMapsDisabled is returned when the user turned map fetching off. The
// bubble still has the sender's embedded thumbnail to show.
var ErrMapsDisabled = errors.New("map fetching is disabled")

// MapUserAgent identifies whatevr to the tile server. OSM's tile usage policy
// requires an identifying User-Agent with a way to reach the maintainers, and
// sending a generic one is how an application gets the whole project blocked.
// main overwrites the version at startup.
var MapUserAgent = "whatevrd/dev (+https://github.com/codelif/whatevr)"

// SetMapUserAgent replaces the identity sent to tile servers. Called once at
// startup so the version in it is the real build's.
func SetMapUserAgent(version string) {
	version = strings.TrimSpace(version)
	if version == "" {
		return
	}
	MapUserAgent = fmt.Sprintf("whatevrd/%s (+https://github.com/codelif/whatevr)", version)
}

// mapTileCoord is one tile in the slippy-map scheme.
type mapTileCoord struct {
	Z, X, Y int
}

// mapFetcher owns tile retrieval and the on-disk tile cache. One per client.
type mapFetcher struct {
	cacheDir    string
	userAgent   string
	urlTemplate string
	client      *http.Client

	// inflight collapses concurrent requests for the same tile, which happens
	// constantly: two locations in the same neighbourhood share most of a grid.
	inflightMu sync.Mutex
	inflight   map[mapTileCoord]*sync.WaitGroup
}

func newMapFetcher(cacheDir, userAgent, urlTemplate string) *mapFetcher {
	if strings.TrimSpace(urlTemplate) == "" {
		urlTemplate = DefaultMapTileURLTemplate
	}
	return &mapFetcher{
		cacheDir:    cacheDir,
		userAgent:   userAgent,
		urlTemplate: urlTemplate,
		client:      &http.Client{Timeout: mapTileFetchTimeout},
		inflight:    make(map[mapTileCoord]*sync.WaitGroup),
	}
}

// StitchMap renders a map centred on lat/lng and writes it to outputPath as a
// PNG. progress, when non-nil, is called after each tile lands with the number
// of tiles done and the total, so the caller can drive the ordinary transfer
// counters.
func (f *mapFetcher) StitchMap(ctx context.Context, lat, lng float64, trail []mapPoint, outputPath string, progress func(done, total int)) error {
	centreX, centreY := projectToPixels(lat, lng, mapZoom)

	// The tile grid is centred on the pin, so the pin sits in the middle tile
	// and the crop below has whole tiles on every side of it.
	originTileX := int(math.Floor(centreX/mapTileSize)) - mapTilesX/2
	originTileY := int(math.Floor(centreY/mapTileSize)) - mapTilesY/2

	canvas := image.NewRGBA(image.Rect(0, 0, mapTilesX*mapTileSize, mapTilesY*mapTileSize))
	// Fill with OSM's land colour so a tile that fails to load reads as a gap in
	// a map rather than a black hole.
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{color.RGBA{0xf2, 0xef, 0xe9, 0xff}}, image.Point{}, draw.Src)

	total := mapTilesX * mapTilesY
	done := 0
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		firstEr error
	)
	for ty := 0; ty < mapTilesY; ty++ {
		for tx := 0; tx < mapTilesX; tx++ {
			wg.Add(1)
			go func(tx, ty int) {
				defer wg.Done()
				coord := mapTileCoord{Z: mapZoom, X: originTileX + tx, Y: originTileY + ty}
				tile, err := f.tile(ctx, coord)
				mu.Lock()
				defer mu.Unlock()
				done++
				if progress != nil {
					progress(done, total)
				}
				if err != nil {
					// A missing tile is a hole in the picture, not a failure of
					// the whole map: the rest is still worth showing. Only a
					// wholesale failure (checked after the wait) is an error.
					if firstEr == nil {
						firstEr = err
					}
					return
				}
				at := image.Pt(tx*mapTileSize, ty*mapTileSize)
				draw.Draw(canvas, image.Rectangle{Min: at, Max: at.Add(image.Pt(mapTileSize, mapTileSize))}, tile, tile.Bounds().Min, draw.Src)
			}(tx, ty)
		}
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}

	// The pin's position within the canvas, then the crop window around it.
	pinX := centreX - float64(originTileX*mapTileSize)
	pinY := centreY - float64(originTileY*mapTileSize)
	cropMinX := clampInt(int(pinX)-mapOutputWidth/2, 0, canvas.Bounds().Dx()-mapOutputWidth)
	cropMinY := clampInt(int(pinY)-mapOutputHeight/2, 0, canvas.Bounds().Dy()-mapOutputHeight)
	crop := image.Rect(cropMinX, cropMinY, cropMinX+mapOutputWidth, cropMinY+mapOutputHeight)

	out := image.NewRGBA(image.Rect(0, 0, mapOutputWidth, mapOutputHeight))
	draw.Draw(out, out.Bounds(), canvas, crop.Min, draw.Src)

	// The trail comes before the pin so a live share's path runs underneath its
	// current position rather than over it.
	drawTrail(out, trail, originTileX, originTileY, crop.Min)
	drawPin(out, image.Pt(int(pinX)-crop.Min.X, int(pinY)-crop.Min.Y))

	if firstEr != nil && done == 0 {
		return firstEr
	}
	return writePNGAtomic(outputPath, out)
}

// mapPoint is one position on a live-location trail.
type mapPoint struct {
	Lat, Lng float64
}

// tile returns one tile, from the cache when it is there and from the network
// otherwise. Concurrent callers for the same tile wait for the first one.
func (f *mapFetcher) tile(ctx context.Context, coord mapTileCoord) (image.Image, error) {
	path := f.tilePath(coord)
	if img, err := decodeImageFile(path); err == nil {
		return img, nil
	}

	f.inflightMu.Lock()
	if wg, ok := f.inflight[coord]; ok {
		f.inflightMu.Unlock()
		wg.Wait()
		return decodeImageFile(path)
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	f.inflight[coord] = wg
	f.inflightMu.Unlock()
	defer func() {
		f.inflightMu.Lock()
		delete(f.inflight, coord)
		f.inflightMu.Unlock()
		wg.Done()
	}()

	body, err := f.fetchTile(ctx, coord)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := writeFileAtomic(path, body, 0o600); err != nil {
		return nil, err
	}
	return decodeImageFile(path)
}

func (f *mapFetcher) fetchTile(ctx context.Context, coord mapTileCoord) ([]byte, error) {
	url := f.tileURL(coord)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// OSM's tile usage policy requires an identifying User-Agent. Sending a
	// generic one is how a client gets the whole project blocked.
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "image/png,image/*")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tile %d/%d/%d: %s", coord.Z, coord.X, coord.Y, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, mapTileMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > mapTileMaxBytes {
		return nil, fmt.Errorf("tile %d/%d/%d is larger than %d bytes", coord.Z, coord.X, coord.Y, mapTileMaxBytes)
	}
	return body, nil
}

func (f *mapFetcher) tileURL(coord mapTileCoord) string {
	replacer := strings.NewReplacer(
		"{z}", fmt.Sprint(coord.Z),
		"{x}", fmt.Sprint(coord.X),
		"{y}", fmt.Sprint(coord.Y),
	)
	return replacer.Replace(f.urlTemplate)
}

func (f *mapFetcher) tilePath(coord mapTileCoord) string {
	return filepath.Join(f.cacheDir, "maps", "tiles",
		fmt.Sprint(coord.Z), fmt.Sprint(coord.X), fmt.Sprintf("%d.tile", coord.Y))
}

// projectToPixels converts WGS84 degrees to Web Mercator pixel coordinates at a
// zoom level: the standard slippy-map projection every raster tile server uses.
func projectToPixels(lat, lng float64, zoom int) (x, y float64) {
	// Clamp to the projection's own limits; Mercator cannot represent the poles.
	lat = math.Max(-85.05112878, math.Min(85.05112878, lat))
	worldSize := float64(mapTileSize) * math.Exp2(float64(zoom))
	x = (lng + 180) / 360 * worldSize
	sinLat := math.Sin(lat * math.Pi / 180)
	y = (0.5 - math.Log((1+sinLat)/(1-sinLat))/(4*math.Pi)) * worldSize
	return x, y
}

// drawPin marks the shared position: a filled dot with a white ring, which
// reads on both a pale street and a dark park.
func drawPin(img *image.RGBA, at image.Point) {
	const (
		outerRadius = 9
		innerRadius = 5
	)
	accent := color.RGBA{0x25, 0xd3, 0x66, 0xff}
	ring := color.RGBA{0xff, 0xff, 0xff, 0xff}
	for dy := -outerRadius; dy <= outerRadius; dy++ {
		for dx := -outerRadius; dx <= outerRadius; dx++ {
			distance := math.Hypot(float64(dx), float64(dy))
			switch {
			case distance <= innerRadius:
				setPixel(img, at.X+dx, at.Y+dy, accent)
			case distance <= outerRadius:
				setPixel(img, at.X+dx, at.Y+dy, ring)
			}
		}
	}
}

// drawTrail joins a live share's positions with a line, so the bubble shows
// where someone has been rather than only where they are.
func drawTrail(img *image.RGBA, trail []mapPoint, originTileX, originTileY int, cropOrigin image.Point) {
	if len(trail) < 2 {
		return
	}
	accent := color.RGBA{0x25, 0xd3, 0x66, 0xcc}
	toCanvas := func(p mapPoint) image.Point {
		x, y := projectToPixels(p.Lat, p.Lng, mapZoom)
		return image.Pt(
			int(x-float64(originTileX*mapTileSize))-cropOrigin.X,
			int(y-float64(originTileY*mapTileSize))-cropOrigin.Y,
		)
	}
	previous := toCanvas(trail[0])
	for _, point := range trail[1:] {
		current := toCanvas(point)
		drawLine(img, previous, current, accent)
		previous = current
	}
}

// drawLine is Bresenham with a little thickness, which is all a trail needs.
func drawLine(img *image.RGBA, from, to image.Point, c color.RGBA) {
	dx := absInt(to.X - from.X)
	dy := -absInt(to.Y - from.Y)
	stepX, stepY := -1, -1
	if from.X < to.X {
		stepX = 1
	}
	if from.Y < to.Y {
		stepY = 1
	}
	err := dx + dy
	for {
		for oy := -1; oy <= 1; oy++ {
			for ox := -1; ox <= 1; ox++ {
				setPixel(img, from.X+ox, from.Y+oy, c)
			}
		}
		if from == to {
			return
		}
		doubled := 2 * err
		if doubled >= dy {
			err += dy
			from.X += stepX
		}
		if doubled <= dx {
			err += dx
			from.Y += stepY
		}
	}
}

func setPixel(img *image.RGBA, x, y int, c color.RGBA) {
	if !(image.Point{x, y}).In(img.Bounds()) {
		return
	}
	img.SetRGBA(x, y, c)
}

func decodeImageFile(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	return img, err
}

func writePNGAtomic(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".map-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := png.Encode(temp, img); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

func clampInt(value, low, high int) int {
	if high < low {
		return low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
