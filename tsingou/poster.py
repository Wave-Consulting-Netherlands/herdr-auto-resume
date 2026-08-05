"""
One sheet. All thirty-two modes, the whole run, as shading.

The film collapses modes 6 through 32 into a single lane because six lanes
fit on a screen. This is the version that does not collapse anything: one
stave per mode, top to bottom, seven and a half minutes left to right.

The shade is (mode energy / total energy) raised to 0.4, so that fractions
of a percent are still visible as smoke. Even with that much help in their
favour, the bottom two thirds of the sheet stay nearly blank for the entire
run. That blankness is the result. Equipartition says those staves should
have filled and stayed filled. They never do, and at 153 periods almost
everything walks back up to the top stave where it started.
"""
import numpy as np
import os
import render
import glyphs

HERE = os.path.dirname(os.path.abspath(__file__))
STATE = os.path.join(HERE, "state")

W, H = 2400, 1500
ML, MR = 236, 2296
TOP = 300
LANE_H, LANE_GAP = 32, 3
GAMMA = 0.4

PAPER = render.PAPER
INK = render.INK
ACCENT = render.ACCENT


def main():
    E = np.load(os.path.join(STATE, "energies.npy")).astype(np.float64)
    meta = np.load(os.path.join(STATE, "meta.npy"), allow_pickle=True).item()
    N, periods = meta["N"], meta["PERIODS"]
    ret_p, ret_f = meta["return_period"], meta["return_fraction"]

    frac = E / E.sum(axis=1, keepdims=True)
    ncol = MR - ML
    # each output column is the mean of the control samples it spans
    edges = np.linspace(0, frac.shape[0], ncol + 1).astype(int)
    band = np.empty((N, ncol))
    for c in range(ncol):
        band[:, c] = frac[edges[c]:max(edges[c + 1], edges[c] + 1)].mean(axis=0)
    shade = np.clip(band, 0, 1) ** GAMMA

    print("peak shade per mode (modes 1-12): " +
          " ".join("%.3f" % z for z in band.max(axis=1)[:12]))
    print("modes 9-32 hold at most %.4f of the energy at any instant"
          % band[8:].max())

    page = np.empty((H, W, 3), dtype=np.float32)
    page[:] = PAPER

    bot = TOP + N * LANE_H
    for k in range(N):
        y = TOP + k * LANE_H
        col = ACCENT if k == 0 else INK
        a = shade[k][None, :, None]
        blk = page[y:y + LANE_H - LANE_GAP, ML:MR]
        blk *= (1.0 - a)
        blk += a * col

    furn = np.zeros((H, W), dtype=np.float32)
    for k in range(N):
        y = TOP + k * LANE_H + LANE_H - LANE_GAP
        furn[y:y + 1, ML:MR] = 0.10
    furn[TOP - 1:TOP, ML:MR] = 0.45
    furn[bot - LANE_GAP:bot - LANE_GAP + 1, ML:MR] = 0.45

    p = 0
    while p <= periods:
        x = int(round(ML + (MR - 1 - ML) * p / periods))
        big = (p % 50 == 0)
        furn[TOP - 15 if big else TOP - 9:TOP - 1, x:x + 1] = 0.45 if big else 0.28
        furn[bot - LANE_GAP:bot - LANE_GAP + (15 if big else 9), x:x + 1] = \
            0.45 if big else 0.28
        p += 10
    render.over(page, furn, INK)

    # the return
    xr = int(round(ML + (MR - 1 - ML) * ret_p / periods))
    mark = np.zeros((H, W), dtype=np.float32)
    mark[TOP - 36:TOP - 1, xr:xr + 2] = 0.85
    mark[TOP:bot - LANE_GAP, xr:xr + 1] = 0.30
    render.over(page, mark, ACCENT)
    lab = "%.0f PERIODS. %.0f PERCENT BACK IN MODE 1." % (ret_p, 100 * ret_f)
    c = render.text_cov(lab, scale=3)
    render.blit(page, c, xr - 16 - c.shape[1], TOP - 32, ACCENT, 0.95)

    def line(text, x, y, scale, alpha, color=INK, right=False):
        c = render.text_cov(text, scale=scale)
        render.blit(page, c, (x - c.shape[1]) if right else x, y, color, alpha)

    line("TSINGOU", ML, 96, 9, 0.88)
    line("THIRTY-TWO MODES OF A NONLINEAR CHAIN, 170 PERIODS", ML, 176, 3, 0.62)
    line("FERMI, PASTA, ULAM AND TSINGOU, LOS ALAMOS 1955", ML, 212, 3, 0.62)
    line("N = 32   ALPHA = 1/4   ALL ENERGY IN MODE 1 AT T = 0", MR, 176, 3, 0.62, right=True)
    line("RE-INTEGRATED, VELOCITY VERLET, 675000 STEPS", MR, 212, 3, 0.62, right=True)

    for k in range(N):
        if k == 0 or (k + 1) % 4 == 0:
            c = render.text_cov(str(k + 1), scale=2)
            y = TOP + k * LANE_H + (LANE_H - LANE_GAP - c.shape[0]) // 2
            render.blit(page, c, ML - 16 - c.shape[1], y, INK, 0.60)

    line("SHADE = (MODE ENERGY / TOTAL ENERGY) ^ 0.4", ML, bot + 34, 3, 0.55)
    line("EQUIPARTITION NEVER ARRIVES", MR, bot + 34, 3, 0.55, right=True)
    line("PROGRAMMED BY MARY TSINGOU ON THE MANIAC I. HER NAME WAS NOT ON THE PAPER.",
         ML, H - 66, 2, 0.45)

    out = os.path.join(HERE, "tsingou-sheet.png")
    render.write_png(out, np.clip(page * 255.0 + 0.5, 0, 255).astype(np.uint8))
    print("wrote", out, "(%.2f MB)" % (os.path.getsize(out) / 1e6))


if __name__ == "__main__":
    main()
