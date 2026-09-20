// Lifted from pawbar's internal/textrun, which is where this pipeline was
// built and proven. Kept close to that copy on purpose: the two should stay
// diffable.

package textrun

import (
	stdlog "log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/fontscan"
	"github.com/go-text/typesetting/shaping"
	"go.rockorager.dev/vaxis/log"
	"golang.org/x/image/math/fixed"
)

// termFamily is the name the terminal's own face is registered under, so a
// query can ask for it by name however kitty found it.
const termFamily = "whattui-term"

// scriptFamilies name a face per script so a phrase gets a typeface drawn for
// it rather than whatever the system fallback lands on. The user's kitty
// symbol_map wins over this, and anything missing falls through to fontscan's
// own script-aware search.
var scriptFamilies = []struct {
	tbl  *unicode.RangeTable
	fams []string
}{
	{unicode.Devanagari, []string{"Noto Sans Devanagari", "Noto Serif Devanagari"}},
	{unicode.Bengali, []string{"Noto Sans Bengali"}},
	{unicode.Gurmukhi, []string{"Noto Sans Gurmukhi"}},
	{unicode.Gujarati, []string{"Noto Sans Gujarati"}},
	{unicode.Oriya, []string{"Noto Sans Oriya"}},
	{unicode.Tamil, []string{"Noto Sans Tamil"}},
	{unicode.Telugu, []string{"Noto Sans Telugu"}},
	{unicode.Kannada, []string{"Noto Sans Kannada"}},
	{unicode.Malayalam, []string{"Noto Sans Malayalam"}},
	{unicode.Sinhala, []string{"Noto Sans Sinhala"}},
	{unicode.Thai, []string{"Noto Sans Thai"}},
	{unicode.Lao, []string{"Noto Sans Lao"}},
	{unicode.Khmer, []string{"Noto Sans Khmer"}},
	{unicode.Myanmar, []string{"Noto Sans Myanmar"}},
	{unicode.Tibetan, []string{"Noto Serif Tibetan"}},
	{unicode.Arabic, []string{"Noto Sans Arabic", "Noto Naskh Arabic"}},
	{unicode.Hebrew, []string{"Noto Sans Hebrew"}},
	{unicode.Syriac, []string{"Noto Sans Syriac"}},
	{unicode.Thaana, []string{"Noto Sans Thaana"}},
}

// fontset owns the font index and the metrics every run is laid out against.
// Everything on it is read-only once built except the caches, which have
// their own lock.
type fontset struct {
	fm      *fontscan.FontMap
	met     metrics
	cellW   int
	cellH   int
	size    fixed.Int26_6
	ellipse string

	mu     sync.Mutex
	seg    shaping.Segmenter
	hb     shaping.HarfbuzzShaper
	wrap   shaping.LineWrapper
	runs   map[runKey]*Run
	images map[string]*cached
}

type runKey struct {
	text   string
	aspect font.Aspect
}

// newShaper builds the font index and measures the cell. It runs off the
// render path, and returns nil if there is no usable font at all.
func newFontset(o Options, cw, ch int) *fontset {
	// fontscan logs to stderr by default, which in a panel is the bar's log
	// stream; send it where the rest of our noise goes.
	fm := fontscan.NewFontMap(stdlog.New(debugWriter{}, "", 0))
	cache, err := os.UserCacheDir()
	if err == nil {
		cache = filepath.Join(cache, "whattui")
	}
	if err := fm.UseSystemFonts(cache); err != nil {
		log.Warn("textrun: no system fonts: %v", err)
		return nil
	}

	s := &fontset{fm: fm, cellW: cw, cellH: ch, ellipse: "…",
		runs: map[runKey]*Run{}, images: map[string]*cached{}}

	if m, err := probe(o, ch); err == nil {
		s.met = *m
		log.Debug("textrun: kitty says em %.1fpx, baseline %d, face %q",
			m.em, m.baseline, m.mediumPath)
	} else {
		log.Debug("textrun: %v, deriving metrics from the resolved face", err)
		s.met.family = o.Family
	}

	// Register the terminal's own face under a name a query can ask for, so
	// latin and punctuation inside a phrase keep the bar's typeface.
	if s.met.mediumPath != "" {
		if f, err := os.Open(s.met.mediumPath); err == nil {
			if err := fm.AddFont(f, s.met.mediumPath, termFamily); err != nil {
				log.Debug("textrun: %q unusable: %v", s.met.mediumPath, err)
			}
			f.Close()
		}
	}

	if s.met.em == 0 || s.met.baseline == 0 {
		s.deriveMetrics()
	}
	s.met.baseline += o.BaselineNudge
	s.size = fixed.I(int(math.Round(s.met.em)))
	return s
}

// deriveMetrics reproduces what kitty does when it sizes a cell: the em is
// whatever makes the face's own ascent plus descent fill the cell, and every
// face then shares the baseline that puts.
func (s *fontset) deriveMetrics() {
	face := s.faceFor('M', nil)
	if face == nil {
		s.met.em = float64(s.cellH) * 0.8
		s.met.baseline = s.cellH * 3 / 4
		return
	}
	ext, ok := face.FontHExtents()
	upem := float64(face.Upem())
	asc := float64(ext.Ascender)
	desc := -float64(ext.Descender)
	if !ok || upem <= 0 || asc+desc <= 0 {
		s.met.em = float64(s.cellH) * 0.8
		s.met.baseline = s.cellH * 3 / 4
		return
	}
	s.met.em = float64(s.cellH) / ((asc + desc) / upem)
	s.met.baseline = int(math.Ceil(asc / upem * s.met.em))
}

// families is the query for a run: the terminal's own face first so shared
// characters do not change typeface, then whatever the user or we named for
// the script it is written in.
func (s *fontset) families(text string) []string {
	out := []string{termFamily}
	if s.met.family != "" {
		out = append(out, s.met.family)
	}
	seen := map[string]bool{}
	for _, r := range text {
		if !isHostile(r) {
			continue
		}
		if fam := s.met.familyFor(r); fam != "" && !seen[fam] {
			seen[fam] = true
			out = append(out, fam)
		}
		for _, sf := range scriptFamilies {
			if !unicode.Is(sf.tbl, r) {
				continue
			}
			for _, fam := range sf.fams {
				if !seen[fam] {
					seen[fam] = true
					out = append(out, fam)
				}
			}
		}
	}
	return out
}

func (s *fontset) faceFor(r rune, families []string) *font.Face {
	if families == nil {
		families = []string{termFamily, fontscan.Monospace}
	}
	s.fm.SetQuery(fontscan.Query{Families: families})
	return s.fm.ResolveFace(r)
}

// Run is one shaped complex phrase, measured in cells and cuttable at the
// cluster boundaries the shaper itself reordered around.
type Run struct {
	s      *fontset
	text   string
	runes  []rune
	aspect font.Aspect
	fams   []string
	// cuts are rune indices of the cluster boundaries, ending at len(runes).
	cuts []int
	// adv is the advance of each cluster, for choosing how much of the phrase
	// survives a trim without reshaping it once per candidate. Subpixel, and
	// it has to stay that way: rounding per cluster costs half a pixel each
	// and a long phrase then measures cells wider than it draws, which is
	// dead space between the ellipsis and whatever comes after it.
	adv []fixed.Int26_6
	// width is the phrase's real advance, ellipse the width of one ellipsis.
	width   fixed.Int26_6
	ellipse fixed.Int26_6
	cells   int
	used    bool
}

// Shape measures text, which must be a complex part from [Split]. It returns
// nil while the shaper is still coming up, so the caller falls back to plain
// terminal cells for that frame and repaints when Notify fires. A nil Shaper
// never shapes, which is what a terminal that cannot place graphics gets.
func (sh *Shaper) Shape(text string, bold, italic bool) *Run {
	if sh == nil {
		return nil
	}
	s, ok := sh.ready()
	if !ok {
		return nil
	}
	key := runKey{text, aspectOf(bold, italic)}

	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[key]; ok {
		r.used = true
		return r
	}
	r := s.measure(text, key.aspect)
	r.used = true
	sweep(s.runs, cacheCap, func(v *Run) *bool { return &v.used })
	s.runs[key] = r
	return r
}

// Cells is how many columns the phrase needs. The slack of up to a cell falls
// at its right edge, where the layout already leaves a gap.
func (r *Run) Cells() int {
	if r == nil {
		return 0
	}
	return r.cells
}

// measure shapes the whole phrase once and reads the cluster boundaries and
// per-cluster advances back off the glyphs. Callers hold s.mu.
func (s *fontset) measure(text string, aspect font.Aspect) *Run {
	r := &Run{s: s, text: text, runes: []rune(text), aspect: aspect}
	r.fams = s.families(text)

	line := s.shapeLine(r.runes, r.fams, aspect)

	// Cluster indices are rune offsets into the input, and they are the only
	// places it is safe to cut: reordering never crosses one.
	adv := map[int]fixed.Int26_6{}
	for _, out := range line {
		for _, g := range out.Glyphs {
			adv[g.ClusterIndex] += g.XAdvance
		}
		r.width += out.Advance
	}
	for idx := range adv {
		r.cuts = append(r.cuts, idx)
	}
	sort.Ints(r.cuts)
	for _, idx := range r.cuts {
		r.adv = append(r.adv, adv[idx])
	}
	r.cuts = append(r.cuts, len(r.runes))

	ell := s.shapeLine([]rune(s.ellipse), r.fams, aspect)
	for _, out := range ell {
		r.ellipse += out.Advance
	}

	r.cells = (r.width.Ceil() + s.cellW - 1) / s.cellW
	if r.cells < 1 {
		r.cells = 1
	}
	return r
}

// shapeLine shapes runes into visually ordered runs, one per script, face and
// bidi level. Callers hold s.mu.
func (s *fontset) shapeLine(runes []rune, families []string, aspect font.Aspect) shaping.Line {
	if len(runes) == 0 {
		return nil
	}
	s.fm.SetQuery(fontscan.Query{Families: families, Aspect: aspect})
	in := shaping.Input{
		Text:     runes,
		RunStart: 0,
		RunEnd:   len(runes),
		Size:     s.size,
	}
	inputs := s.seg.Split(in, s.fm)
	if len(inputs) == 0 {
		return nil
	}

	line := make(shaping.Line, len(inputs))
	for i, sub := range inputs {
		line[i] = s.hb.Shape(sub)
	}

	// Runs come out in logical order with a direction each; the wrapper is
	// what turns that into the order they are drawn in.
	s.wrap.Prepare(shaping.WrapConfig{Direction: line[0].Direction}, runes, shaping.NewSliceIterator(line))
	wrapped, _ := s.wrap.WrapNextLine(math.MaxInt)
	line = wrapped.Line
	sort.SliceStable(line, func(i, j int) bool { return line[i].VisualIndex < line[j].VisualIndex })
	return line
}

// debugWriter funnels a library's stdlib logger into ours.
type debugWriter struct{}

func (debugWriter) Write(p []byte) (int, error) {
	log.Debug("textrun: %s", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func aspectOf(bold, italic bool) font.Aspect {
	a := font.Aspect{Weight: font.WeightNormal, Style: font.StyleNormal}
	if bold {
		a.Weight = font.WeightBold
	}
	if italic {
		a.Style = font.StyleItalic
	}
	return a
}

// Prefix is the longest run of clusters that fits in cells columns, as text.
// Clusters are the only safe granularity: cut anywhere else and a matra is
// stranded from its base or a virama is left hanging.
//
// The caller reshapes what comes back rather than reusing this run, which is
// what makes a clipped phrase a correctly shaped shorter phrase instead of a
// correct phrase with its right hand side missing.
func (r *Run) Prefix(cells int) string {
	if r == nil || cells < 1 {
		return ""
	}
	if cells >= r.cells {
		return r.text
	}
	budget := fixed.I(cells * r.s.cellW)
	used := fixed.Int26_6(0)
	hi := 0
	for hi < len(r.adv) && used+r.adv[hi] <= budget {
		used += r.adv[hi]
		hi++
	}

	// The per-cluster advances are what the clusters measured inside the whole
	// phrase. Cutting changes their context: a conjunct that lost its second
	// half reshapes to two wider glyphs, so the sum is a starting guess and
	// not an answer. Reshape and give a cluster back until it really fits.
	s := r.s
	s.mu.Lock()
	defer s.mu.Unlock()
	for ; hi > 0; hi-- {
		text := string(r.runes[:r.cuts[hi]])
		if s.cellsOf(text, r.fams, r.aspect) <= cells {
			return text
		}
	}
	return ""
}

// cellsOf is how many columns text takes once shaped. Callers hold s.mu.
func (s *fontset) cellsOf(text string, fams []string, aspect font.Aspect) int {
	if text == "" {
		return 0
	}
	var w fixed.Int26_6
	for _, out := range s.shapeLine([]rune(text), fams, aspect) {
		w += out.Advance
	}
	n := (w.Ceil() + s.cellW - 1) / s.cellW
	if n < 1 {
		return 1
	}
	return n
}

// Text is what was shaped.
func (r *Run) Text() string {
	if r == nil {
		return ""
	}
	return r.text
}
