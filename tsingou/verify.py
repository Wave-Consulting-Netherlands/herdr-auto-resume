"""
Check that the sound is actually the physics.

It is easy to build a synthesis chain that produces something plausible and
wrong. This pulls the 110 Hz partial back out of the finished audio file by
heterodyne detection -- multiply by sin and cos at 110 Hz, integrate over
half a second, take the magnitude -- and compares that measured envelope
against sqrt(E_1(t)) straight from the integration.

They should be the same curve to within the window's resolution. If they are
not, something between the integrator and the file is lying.

Also checks the two things that were wrong at first and would be easy to
break again: that loudness stays flat, since total energy is conserved and
nothing should read as a dynamic arc, and that the stereo image survives a
mono fold-down.
"""
import numpy as np
import os

HERE = os.path.dirname(os.path.abspath(__file__))
STATE = os.path.join(HERE, "state")
SR = 48000
F0 = 110.0


def main():
    a = np.fromfile(os.path.join(STATE, "audio.f32"), dtype=np.float32).reshape(-1, 2)
    E = np.load(os.path.join(STATE, "energies.npy")).astype(np.float64)
    meta = np.load(os.path.join(STATE, "meta.npy"), allow_pickle=True).item()
    control_hz = meta["CONTROL_HZ"]
    mono = a.mean(axis=1)

    # --- is the 110 Hz partial the mode-1 energy? ---
    W, hop = int(0.5 * SR), int(2.0 * SR)
    skip = int((meta.get("FADE_IN", 2.0) + 2.0) * SR)
    ts = np.arange(skip, len(mono) - W, hop)
    win = np.hanning(W)
    n = np.arange(W) / SR
    amp = np.array([
        2 * np.hypot(
            (mono[t:t + W] * win * np.cos(2 * np.pi * F0 * (t / SR + n))).sum(),
            (mono[t:t + W] * win * np.sin(2 * np.pi * F0 * (t / SR + n))).sum(),
        ) / win.sum() for t in ts])
    pred = np.sqrt(E[(ts * control_hz // SR).astype(int), 0])
    pred *= (amp * pred).sum() / (pred * pred).sum()

    corr = np.corrcoef(amp, pred)[0, 1]
    print("110 Hz partial vs sqrt(E_1) from the integration")
    print("  correlation    %.6f" % corr)
    print("  max deviation  %.2f%% of peak" % (100 * np.abs(amp - pred).max() / amp.max()))
    print("  rms deviation  %.3f%% of peak"
          % (100 * np.sqrt(((amp - pred) ** 2).mean()) / amp.max()))
    assert corr > 0.999, "synthesised partial does not track the mode energy"

    # --- does the loudness stay put? ---
    Wr = 1 << 15
    r = np.array([[np.sqrt((a[t:t + Wr] ** 2).mean()),
                   np.sqrt((mono[t:t + Wr] ** 2).mean())]
                  for t in range(skip, len(a) - Wr, 5 * SR)])
    spread = 20 * np.log10(r[:, 0].max() / r[:, 0].min())
    fold = (20 * np.log10(r[:, 1] / r[:, 0])).min()
    print("\nloudness spread over the run   %.2f dB   (energy is conserved)" % spread)
    print("worst mono fold-down loss      %.2f dB" % fold)
    print("peak                           %.3f" % np.abs(a).max())
    assert spread < 1.0, "loudness is moving; the physics says it should not"
    assert fold > -1.5, "partials cancelling in mono"
    assert np.abs(a).max() < 1.0, "clipping"

    i = int(np.argmax(amp[len(amp) // 2:])) + len(amp) // 2
    print("\nmode 1 is loudest again at %.0f s (%d:%02d)"
          % (ts[i] / SR, ts[i] / SR // 60, ts[i] / SR % 60))
    print("\nall checks passed")


if __name__ == "__main__":
    main()
