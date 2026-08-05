"""
Fermi-Pasta-Ulam-Tsingou alpha-chain, integrated and recorded.

A string of 32 masses, springs slightly nonlinear. All the energy is put into
the lowest mode. It is supposed to thermalise -- to spread into all 32 modes
and stay there. It does not. It wanders for 157 fundamental periods and then
comes almost all the way back.

Mary Tsingou wrote the program that found this, on the MANIAC I at Los Alamos
in 1953. Her name was left off the paper.

This script re-runs it and writes out the state, sampled at a fixed control
rate, for the renderers.
"""
import numpy as np
import os

N = 32              # interior masses
ALPHA = 0.25        # nonlinearity, as in the original
PERIODS = 170.0     # length of the run, in periods of mode 1
CONTROL_HZ = 300.0  # samples per second of *piece* time
PIECE_SECONDS = 450.0
SUBSTEPS = 5        # integrator steps per control sample

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "state")

i = np.arange(1, N + 1)
k = np.arange(1, N + 1)
omega = 2.0 * np.sin(k * np.pi / (2 * (N + 1)))
S = np.sin(np.outer(i, k) * np.pi / (N + 1))
NORM = np.sqrt(2.0 / (N + 1))
T1 = 2 * np.pi / omega[0]


def accel(x):
    xf = np.empty(N + 2)
    xf[0] = 0.0
    xf[-1] = 0.0
    xf[1:-1] = x
    d = np.diff(xf)
    return (d[1:] - d[:-1]) + ALPHA * (d[1:] ** 2 - d[:-1] ** 2)


def run():
    n_control = int(round(PIECE_SECONDS * CONTROL_HZ))
    total_time = PERIODS * T1
    dt = total_time / (n_control * SUBSTEPS)
    print("N=%d alpha=%.3f  T1=%.4f  dt=%.6f  control samples=%d"
          % (N, ALPHA, T1, dt, n_control))

    x = np.sin(i * np.pi / (N + 1))
    v = np.zeros(N)
    a = accel(x)

    energies = np.empty((n_control, N), dtype=np.float64)
    disp = np.empty((n_control, N), dtype=np.float32)

    half = 0.5 * dt
    for c in range(n_control):
        A = NORM * (S.T @ x)
        Ad = NORM * (S.T @ v)
        energies[c] = 0.5 * (Ad ** 2 + omega ** 2 * A ** 2)
        disp[c] = x
        for _ in range(SUBSTEPS):
            v += half * a
            x += dt * v
            a = accel(x)
            v += half * a
        if c % 20000 == 0:
            print("  %6.1f%%  t=%7.2f periods" % (100.0 * c / n_control,
                                                  c / CONTROL_HZ / PIECE_SECONDS * PERIODS))

    tot = energies.sum(axis=1)
    print("energy conservation: min=%.8f max=%.8f  relative spread=%.2e"
          % (tot.min(), tot.max(), (tot.max() - tot.min()) / tot.mean()))

    frac1 = energies[:, 0] / tot
    lo = int(frac1.argmin())
    win = frac1[lo:]
    pk = lo + int(win.argmax())
    print("mode 1 falls to %.1f%% at %.1f periods" % (100 * frac1[lo], lo / n_control * PERIODS))
    print("mode 1 returns to %.1f%% at %.1f periods  (%.1f s into the piece)"
          % (100 * frac1[pk], pk / n_control * PERIODS, pk / CONTROL_HZ))

    os.makedirs(OUT, exist_ok=True)
    np.save(os.path.join(OUT, "energies.npy"), energies.astype(np.float32))
    np.save(os.path.join(OUT, "disp.npy"), disp)
    np.save(os.path.join(OUT, "omega.npy"), omega)
    meta = dict(N=N, ALPHA=ALPHA, PERIODS=PERIODS, CONTROL_HZ=CONTROL_HZ,
                PIECE_SECONDS=PIECE_SECONDS, T1=T1,
                return_period=pk / n_control * PERIODS,
                return_second=pk / CONTROL_HZ,
                return_fraction=float(frac1[pk]),
                max_disp=float(np.abs(disp).max()))
    np.save(os.path.join(OUT, "meta.npy"), meta, allow_pickle=True)
    print("max |displacement| = %.4f" % meta["max_disp"])
    print("wrote", OUT)


if __name__ == "__main__":
    run()
