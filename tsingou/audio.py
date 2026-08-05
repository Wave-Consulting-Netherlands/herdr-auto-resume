"""
Render the chain as sound.

Each normal mode of the chain becomes one partial. Its frequency is the
chain's own dispersion relation, omega_k = 2 sin(k pi / 2(N+1)), transposed
so that mode 1 lands at 110 Hz -- a series slightly flatter than harmonic,
because a chain of beads is not an ideal string.

Its amplitude is sqrt(E_k(t)): the square root of the energy actually in that
mode at that instant, read straight out of the integration. Nothing is added
and nothing is taken away; total energy is conserved, so the loudness barely
moves for seven and a half minutes. Only the timbre changes. That change is
the entire piece.

Stereo comes from two pickups placed at 0.23 and 0.61 of the way along the
chain. Mode k reaches a pickup weighted by sin(k pi p) -- its own shape at
that point -- so each mode sits somewhere different in the image, and as
energy moves between modes the sound moves across the field.

Two places I overrule the physics, both for the same reason. Real pickups
respond to signed displacement, so modes that arrive out of phase at the two
points would cancel in mono, and real pickups sitting where these sit would
also make the middle of the piece 8 dB quieter purely by accident of
position. Both would read as drama the system does not have. So the pickups
set only the pan angle, by magnitude, at constant power. The pickups say
where. The integration says how much.

All partials start at phase zero, which is not a stylistic choice: the chain
starts at rest, displaced, with zero velocity everywhere. It has to begin
from silence. Over seven minutes the slightly-inharmonic partials drift out
of that alignment on their own.
"""
import numpy as np
import os

HERE = os.path.dirname(os.path.abspath(__file__))
STATE = os.path.join(HERE, "state")

SR = 48000
F0 = 110.0            # mode 1 -> A2
PICKUPS = (0.23, 0.61)
SPREAD = 0.85         # 1.0 = hard-pan a mode that nodes at one pickup
TILT_K = 14.0         # gentle rolloff; high modes radiate less
FADE_IN = 2.0
FADE_OUT = 0.15       # a stop, not an ending
TARGET_PEAK = 0.89
CHUNK = 1 << 20


def render(out_path):
    E = np.load(os.path.join(STATE, "energies.npy")).astype(np.float64)
    omega = np.load(os.path.join(STATE, "omega.npy"))
    meta = np.load(os.path.join(STATE, "meta.npy"), allow_pickle=True).item()

    N = meta["N"]
    dur = meta["PIECE_SECONDS"]
    control_hz = meta["CONTROL_HZ"]
    n = int(round(dur * SR))
    k = np.arange(1, N + 1)

    freqs = F0 * omega / omega[0]
    tilt = 1.0 / np.sqrt(1.0 + (k / TILT_K) ** 2)
    amps = np.sqrt(np.maximum(E, 0.0)) * tilt          # (n_control, N)

    wl = np.abs(np.sin(k * np.pi * PICKUPS[0])) + 1e-6
    wr = np.abs(np.sin(k * np.pi * PICKUPS[1])) + 1e-6
    theta = np.arctan2(wr, wl)
    theta = 0.25 * np.pi + SPREAD * (theta - 0.25 * np.pi)
    gl, gr = np.cos(theta), np.sin(theta)
    print("pan (0=hard L, 1=hard R), modes 1-8: " +
          " ".join("%.2f" % z for z in (theta / (0.5 * np.pi))[:8]))

    print("partials: %.1f Hz .. %.1f Hz" % (freqs[0], freqs[-1]))
    print("ratio to harmonic, modes 1-8: " +
          " ".join("%.4f" % (freqs[j] / freqs[0] / (j + 1)) for j in range(8)))

    t_control = np.arange(E.shape[0]) / control_hz
    buf = np.zeros((n, 2), dtype=np.float64)

    for start in range(0, n, CHUNK):
        stop = min(start + CHUNK, n)
        idx = np.arange(start, stop)
        t = idx / SR
        env = np.empty((stop - start, N))
        for j in range(N):
            env[:, j] = np.interp(t, t_control, amps[:, j])
        # phase kept small by folding: sin(2 pi f t) with t reduced per partial
        for j in range(N):
            ph = 2.0 * np.pi * np.mod(freqs[j] * t, 1.0)
            s = np.sin(ph) * env[:, j]
            buf[start:stop, 0] += gl[j] * s
            buf[start:stop, 1] += gr[j] * s
        print("  synth %5.1f%%" % (100.0 * stop / n), end="\r")
    print()

    fi = int(FADE_IN * SR)
    buf[:fi] *= (0.5 - 0.5 * np.cos(np.linspace(0, np.pi, fi)))[:, None]
    fo = int(FADE_OUT * SR)
    buf[-fo:] *= (0.5 + 0.5 * np.cos(np.linspace(0, np.pi, fo)))[:, None]

    peak = np.abs(buf).max()
    rms = np.sqrt((buf ** 2).mean())
    buf *= TARGET_PEAK / peak
    print("raw peak %.4f  rms %.4f  crest %.1f dB -> scaled to %.2f"
          % (peak, rms, 20 * np.log10(peak / rms), TARGET_PEAK))

    corr = np.corrcoef(buf[::37, 0], buf[::37, 1])[0, 1]
    print("L/R correlation: %.3f" % corr)

    buf.astype(np.float32).tofile(out_path)
    print("wrote", out_path, "(%.1f MB)" % (os.path.getsize(out_path) / 1e6))


if __name__ == "__main__":
    render(os.path.join(STATE, "audio.f32"))
