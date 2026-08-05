// Mediant.
//
// A piece for 257 sine tones, rendered to a WAV file with nothing but the
// Go standard library.
//
// The score is not written; it is derived. Start with the interval between
// 1/1 and 2/1 — an octave, two tones, nothing else. Between every adjacent
// pair of ratios, insert their mediant: (p+r)/(q+s). Do that eight times.
// The Stern-Brocot construction hands back every rational in the octave, in
// lowest terms, each exactly once, in order of increasing arithmetic
// complexity: 3/2 first, then 4/3 and 5/3, then 5/4 7/5 7/4 8/5, and onward
// into 55/34 and its neighbours.
//
// Everything else follows from the fraction itself. A tone's loudness, its
// register, the moment it enters, the rate at which it breathes, the place
// it sits between the speakers — all of it read off p and q. Simple ratios
// are loud, low, early, and slow. Complex ones are quiet, high, late, and
// restless. Nothing is chosen twice.
//
// The form, then, is not a decision either. It is what the number line does
// when you let it fill in: a bare octave, a long accumulation, and finally a
// density that no longer resolves into parts. The tree does not terminate.
// The fade at the end is only where the listening stops.
//
//	go run . -o mediant.wav
package main

import (
	"bufio"
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"sync"
)

// ---------------------------------------------------------------- the lattice

type frac struct{ p, q int }

func (f frac) val() float64    { return float64(f.p) / float64(f.q) }
func (f frac) height() float64 { return math.Log2(float64(f.p * f.q)) }
func (f frac) String() string  { return fmt.Sprintf("%d/%d", f.p, f.q) }

type node struct {
	frac
	depth int // generation at which the mediant appeared
}

// grow returns the Stern-Brocot subtree spanning [1/1, 2/1] after n rounds of
// mediant insertion, in ascending order of value.
func grow(n int) []node {
	list := []node{{frac{1, 1}, 0}, {frac{2, 1}, 0}}
	for d := 1; d <= n; d++ {
		next := make([]node, 0, 2*len(list)-1)
		for i := 0; i < len(list)-1; i++ {
			a, b := list[i], list[i+1]
			next = append(next, a)
			next = append(next, node{frac{a.p + b.p, a.q + b.q}, d})
		}
		next = append(next, list[len(list)-1])
		list = next
	}
	return list
}

// ------------------------------------------------------------------ the score

// The piece holds nothing above 3.2 kHz — 257 pure sines, no harmonics — so
// any rate past ~7 kHz carries the same signal. 44.1 kHz is the default only
// because it is what everything expects.
var rate = 44100

const (
	duration = 420.0 // seconds — seven minutes

	silence = 14.0  // the bare octave, before anything is inserted
	grows   = 300.0 // the accumulation
	release = 46.0  // the fade, which resolves nothing

	root    = 55.0 // A1: the 1/1
	attack  = 7.5  // every tone enters over this long
	ctrlDiv = 64   // control-rate divisor for the breathing
)

// A voice is one sine tone: everything about it derived from one fraction.
type voice struct {
	freq  float64 // root * ratio * 2^octave
	amp   float64 // Tenney-weighted: simple is loud
	birth float64 // seconds; ordered by complexity
	lfoHz float64 // its own slow breath, rate taken from p and q
	lfoPh float64
	phase float64 // fixed at start, deterministic
	gainL float64
	gainR float64
}

func score() ([]voice, []node) {
	nodes := grow(8)

	// Complexity ordering. Ties (and there are many — whole families of
	// ratios arrive together) are broken by position in the octave so the
	// stereo field fills evenly rather than from one side.
	order := make([]int, len(nodes))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := nodes[order[a]], nodes[order[b]]
		if x.height() != y.height() {
			return x.height() < y.height()
		}
		return x.val() < y.val()
	})

	hMax := 0.0
	for _, n := range nodes {
		hMax = math.Max(hMax, n.height())
	}

	// Deterministic phase scatter. A fixed seed: this is a score, not a
	// throw of dice. Numerical Recipes' LCG, because it fits in one line.
	rnd := uint64(20260805)
	next := func() float64 {
		rnd = rnd*6364136223846793005 + 1442695040888963407
		return float64(rnd>>11) / float64(1<<53)
	}

	voices := make([]voice, len(nodes))
	for rank, idx := range order {
		n := nodes[idx]
		h := n.height()

		// Register rises with complexity, so the low end stays legible and
		// the crowding happens overhead.
		oct := math.Floor(h / 2.1)
		freq := root * n.val() * math.Pow(2, oct)

		// Loudness falls with complexity. The exponent is the only number
		// here tuned by ear I do not have — it sets how far down the haze
		// sits relative to the octave holding it up.
		amp := math.Pow(float64(n.p*n.q), -0.62)

		// Arrival. Blend the raw complexity ordering with rank so the late
		// generations, which are enormous, do not all land in one heap.
		byHeight := h / hMax
		byRank := float64(rank) / float64(len(nodes)-1)
		t := 0.55*byHeight + 0.45*math.Pow(byRank, 0.85)
		birth := silence + grows*t
		if n.depth == 0 {
			birth = 0
		}

		// Its breath: a slow amplitude sway at a rate read off the fraction.
		// The periods are mutually awkward, so the texture churns without
		// ever coming back around.
		lfo := 0.008 + 0.0021*float64((n.p+2*n.q)%23)

		// Position between the speakers is position in the octave. 1/1 sits
		// far left, 2/1 far right, 3/2 in the middle — the number line laid
		// out across the room. Equal power, so the middle does not sag.
		pan := 0.5 + 0.8*(n.val()-1.5)
		voices[rank] = voice{
			freq:  freq,
			amp:   amp,
			birth: birth,
			lfoHz: lfo,
			lfoPh: next() * 2 * math.Pi,
			phase: next() * 2 * math.Pi,
			gainL: math.Cos(pan * math.Pi / 2),
			gainR: math.Sin(pan * math.Pi / 2),
		}
	}
	ordered := make([]node, len(nodes))
	for rank, idx := range order {
		ordered[rank] = nodes[idx]
	}
	return voices, ordered
}

// ----------------------------------------------------------------- the render

func render(voices []voice) []float64 {
	total := int(duration * float64(rate))
	buf := make([]float64, 2*total) // interleaved L,R

	workers := runtime.NumCPU()
	span := (total + workers - 1) / workers
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		lo := w * span
		hi := lo + span
		if hi > total {
			hi = total
		}
		if lo >= hi {
			continue
		}
		wg.Add(1)
		go func(lo, hi int) {
			defer wg.Done()
			for _, v := range voices {
				// Skip the stretches where this tone is not yet alive.
				start := int(v.birth * float64(rate))
				if start >= hi {
					continue
				}
				if start < lo {
					start = lo
				}

				dph := 2 * math.Pi * v.freq / float64(rate)
				dlf := 2 * math.Pi * v.lfoHz / float64(rate)

				// Envelope and breath move slowly enough to compute at
				// control rate and slide between.
				var env, denv, brt, dbrt float64
				for i := start; i < hi; i++ {
					if (i-start)%ctrlDiv == 0 {
						t := float64(i) / float64(rate)
						j := i + ctrlDiv
						tn := float64(j) / float64(rate)
						e0 := envelope(t, v.birth)
						e1 := envelope(tn, v.birth)
						env, denv = e0, (e1-e0)/ctrlDiv
						b0 := 1 - 0.42*(1-math.Cos(v.lfoPh+dlf*float64(i)))/2
						b1 := 1 - 0.42*(1-math.Cos(v.lfoPh+dlf*float64(j)))/2
						brt, dbrt = b0, (b1-b0)/ctrlDiv
					}
					s := math.Sin(v.phase+dph*float64(i)) * v.amp * env * brt
					buf[2*i] += s * v.gainL
					buf[2*i+1] += s * v.gainR
					env += denv
					brt += dbrt
				}
			}
		}(lo, hi)
	}
	wg.Wait()
	return buf
}

// envelope: a raised-cosine entrance, an indefinite sustain, and the global
// release that stops the piece without concluding it.
func envelope(t, birth float64) float64 {
	if t < birth {
		return 0
	}
	e := 1.0
	if d := t - birth; d < attack {
		e = (1 - math.Cos(math.Pi*d/attack)) / 2
	}
	if r := duration - release; t > r {
		x := math.Min((t-r)/release, 1)
		e *= math.Pow(1-x, 1.7) // slow at first, then gone
	}
	return e
}

// ------------------------------------------------------------------ the file

func writeWAV(path string, buf []float64) error {
	peak := 0.0
	for _, s := range buf {
		peak = math.Max(peak, math.Abs(s))
	}
	gain := math.Pow(10, -1.0/20) / peak // land at -1 dBFS

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriterSize(f, 1<<20)

	data := uint32(len(buf) * 2)
	hdr := []any{
		[4]byte{'R', 'I', 'F', 'F'}, uint32(36 + data), [4]byte{'W', 'A', 'V', 'E'},
		[4]byte{'f', 'm', 't', ' '}, uint32(16), uint16(1), uint16(2),
		uint32(rate), uint32(rate * 2 * 2), uint16(4), uint16(16),
		[4]byte{'d', 'a', 't', 'a'}, data,
	}
	for _, v := range hdr {
		if err := binary.Write(w, binary.LittleEndian, v); err != nil {
			return err
		}
	}

	// Triangular dither at one bit, so the long fade dissolves into noise
	// instead of stepping down a staircase.
	rnd := uint64(11235813)
	tri := func() float64 {
		a := func() float64 {
			rnd = rnd*6364136223846793005 + 1442695040888963407
			return float64(rnd>>11) / float64(1<<53)
		}
		return a() + a() - 1
	}

	out := make([]byte, 2)
	for _, s := range buf {
		x := s*gain*32767 + tri()
		if x > 32767 {
			x = 32767
		} else if x < -32768 {
			x = -32768
		}
		binary.LittleEndian.PutUint16(out, uint16(int16(math.Round(x))))
		if _, err := w.Write(out); err != nil {
			return err
		}
	}
	return w.Flush()
}

// ------------------------------------------------------------------------ run

func main() {
	out := flag.String("o", "mediant.wav", "output WAV path")
	dump := flag.Bool("dump", false, "print the score and exit")
	flag.IntVar(&rate, "rate", rate, "sample rate")
	flag.Parse()

	voices, nodes := score()

	if *dump {
		fmt.Printf("%d tones\n\n%-9s %-6s %-7s %-9s %-7s %s\n",
			len(nodes), "ratio", "depth", "height", "enters", "Hz", "amp")
		for i, v := range voices {
			n := nodes[i]
			fmt.Printf("%-9s %-6d %-7.2f %6.1fs   %-7.1f %.4f\n",
				n, n.depth, n.height(), v.birth, v.freq, v.amp)
		}
		return
	}

	fmt.Printf("mediant: %d tones, %.0f seconds\n", len(voices), duration)
	buf := render(voices)
	if err := writeWAV(*out, buf); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	st, _ := os.Stat(*out)
	fmt.Printf("wrote %s (%.1f MB)\n", *out, float64(st.Size())/(1<<20))
}
