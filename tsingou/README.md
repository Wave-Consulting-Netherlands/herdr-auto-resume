# Tsingou

A seven-and-a-half minute film with sound, generated entirely from a physics
integration. No recorded material, no image files, no font files, no assets of
any kind. Five Python scripts and ffmpeg.

## What it is

In 1953 at Los Alamos, Enrico Fermi, John Pasta and Stanislaw Ulam wanted to
watch a solid thermalise. They took a string of 64 masses connected by springs,
made the springs very slightly nonlinear, put all the energy into the lowest
vibrational mode, and let it run on the MANIAC I. Statistical mechanics said the
energy would spread into all the modes and stay spread — equipartition, heat
death, the arrow of time.

The program was written by **Mary Tsingou**. She did the coding, and she ran it.
The 1955 report that made the result famous, LA-1940, thanks her in an
acknowledgement and does not carry her name as an author. It is now usually
called the Fermi–Pasta–Ulam–Tsingou problem, which took about fifty years.

The energy did not spread out. It moved into the second, third, fourth modes,
milled around for a while, and then came back. After 157 periods of the
fundamental, almost all of it was in mode 1 again, where it started. The system
recurred. Nobody expected this, and chasing why led to soliton theory and to a
good deal of modern nonlinear dynamics.

This is that run, integrated again, and turned into something you can watch and
hear.

## What you see and hear

Both come from one integration. There is no separate audio track and no
separate animation; the same state vector is read twice.

**Sound.** Each of the 32 normal modes is one partial. Its frequency is the
chain's own dispersion relation, `omega_k = 2 sin(k pi / 2(N+1))`, transposed so
mode 1 lands at 110 Hz — a series very slightly flatter than harmonic, because a
chain of beads is not an ideal string. Its amplitude is `sqrt(E_k(t))`, the
energy actually in that mode at that instant. Because total energy is conserved,
the loudness of the piece varies by **0.26 dB across seven and a half minutes**.
Nothing is added and nothing is taken away. Only the colour changes, and that
change is the whole piece: a nearly pure tone that gets reedy, then complex,
then wanders, then — around 6:45 — resolves back to nearly the pure tone, and
begins to leave again.

Stereo comes from two pickups sitting at 0.23 and 0.61 of the way along the
chain, so each mode arrives at a different place in the image. Everything about
the sound is a consequence of the physics except two decisions, both documented
in `audio.py`: the pickups set pan angle only rather than level, and there is a
gentle high-mode rolloff. Both prevent accidents of pickup placement from
reading as drama the system does not have.

**Picture.** Top of the page: the 32 masses, actual displacement, with a
four-second trail so the standing wave leaves a wake. Bottom: six lanes, drawn
by a pen crossing the page once, left to right, over the length of the piece.
Five lanes are modes 1 to 5. The sixth is every remaining mode, 6 through 32,
added together.

The lane scale is linear and the lanes are not individually normalised. That is
the point. Lane 6+ never gets above 6% and mostly sits near zero. That nearly
flat empty lane, for seven minutes, is the 1955 result.

## The sheet

`tsingou-sheet.png` is the companion still: one stave per mode, all thirty-two
of them, the whole run left to right. The film collapses 6–32 into a single lane
because six lanes fit on a screen; the sheet collapses nothing. Modes 9 through
32 together never hold more than **0.4%** of the energy at any instant, and the
bottom two thirds of the sheet stay blank for the entire run even with the shade
gamma-boosted to make fractions of a percent visible.

## Running it

```
pip install numpy          # the only dependency
python3 sim.py             # integrate; ~13 s, writes state/
python3 audio.py           # synthesise; ~70 s, writes state/audio.f32
python3 poster.py          # the sheet
FPS=60 python3 render.py   # the film; ~35 min, writes tsingou.mp4
python3 render.py --preview  # single frames instead, for checking layout
```

There is no randomness anywhere — not in the integration, not in the partial
phases, not in the rendering. Every run produces the same bytes. That is why the
generated media are not committed here: the code is the piece, and it is a
faithful copy of itself.

## Files

| | |
|---|---|
| `sim.py` | velocity-Verlet integration of the alpha-chain, 675,000 steps, N=32, alpha=1/4 |
| `audio.py` | 32-partial resynthesis from the mode energies, 48 kHz stereo |
| `render.py` | the film: numpy rasteriser piping rgb24 into ffmpeg |
| `poster.py` | the thirty-two-stave sheet |
| `glyphs.py` | a 5x7 bitmap alphabet, hand-set, because there are no font files |

## Notes on two things I got wrong on the way

The pickups originally made the middle of the piece 8 dB quieter than the ends,
purely by accident of where they sat on the chain. It sounded like a shape. It
was not a shape; it was a mistake that flattered the material. Fixing it is what
produced the 0.26 dB figure above.

The alphabet started at 3x5 and had to grow twice. Five rows is not enough
height for a diagonal to read as a diagonal — `N` came out as a Cyrillic И, and
widening it to four columns only turned it into an `M`. 5x7 is the smallest grid
where `N`, `M` and `W` are all themselves.

---

Programmed by Mary Tsingou on the MANIAC I. Her name was not on the paper.
