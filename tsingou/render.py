"""
Draw the chain, and draw the ledger of where its energy is.

Top of the page: the 32 masses, actual displacement, with a trail a few
seconds long so the standing wave leaves a wake.

Bottom of the page: six lanes. The first five are modes 1 through 5. The
sixth is every remaining mode, 6 through 32, added together. The scale is
linear and the lanes are not normalised, which is the point -- the sixth
lane stays nearly empty for the whole seven minutes. That flat empty lane is
the 1955 result. The energy was supposed to end up there and it never goes.

A pen crosses the page once, left to right, over the length of the piece.
"""
import numpy as np
import os
import sys
import subprocess
import glyphs

HERE = os.path.dirname(os.path.abspath(__file__))
STATE = os.path.join(HERE, "state")

W, H = 1920, 1080
ML, MR = 150, 1830

CHAIN_T, CHAIN_B = 108, 566
CY = 337
AMP_PX = 200.0
HALF_W = 1.7
DOT_R = 4.4
TRAIL_SECONDS = 4.0
TRAIL_MAX = 0.26

RULE_Y = 620
LANE_TOP = 664
LANE_H = 58
LANE_GAP = 6
N_LANES = 6
TICK_Y = LANE_TOP + N_LANES * LANE_H

PAPER = np.array([0.953, 0.941, 0.914], dtype=np.float32)
INK = np.array([0.102, 0.098, 0.094], dtype=np.float32)
ACCENT = np.array([0.596, 0.208, 0.141], dtype=np.float32)

HEADER = "FERMI PASTA ULAM TSINGOU. 32 MASSES, NONLINEAR SPRINGS, ALPHA = 1/4."
FOOT_L = "MODE ENERGY / TOTAL ENERGY. LINEAR."
FOOT_R = "MANIAC I, LOS ALAMOS. PROGRAMMED BY MARY TSINGOU."
LANE_LABELS = ["1", "2", "3", "4", "5", "6+"]


def over(dst, cov, color):
    """Composite `color` onto float RGB `dst` with coverage `cov`."""
    c = cov[..., None]
    dst *= (1.0 - c)
    dst += c * color


def text_cov(text, scale=3, tracking=1):
    w = glyphs.width(text, scale, tracking)
    cov = np.zeros((glyphs.GH * scale, w), dtype=np.float32)
    glyphs.stamp(cov, text, 0, 0, scale, tracking)
    return cov


def blit(dst, cov, x, y, color, alpha=1.0):
    h, w = cov.shape
    sub = dst[y:y + h, x:x + w]
    over(sub, cov[:sub.shape[0], :sub.shape[1]] * alpha, color)


def build_page():
    """The blank sheet: paper, rules, lane frames, ticks, type."""
    page = np.empty((H, W, 3), dtype=np.float32)
    page[:] = PAPER

    hair = np.zeros((H, W), dtype=np.float32)
    hair[CY:CY + 1, ML:MR] = 0.16          # zero displacement
    hair[RULE_Y:RULE_Y + 1, ML:MR] = 0.45
    for k in range(N_LANES):
        base = LANE_TOP + k * LANE_H + (LANE_H - LANE_GAP)
        hair[base:base + 1, ML:MR] = 0.40
    over(page, hair, INK)

    # time ticks, every 10 periods of mode 1, taller every 50
    meta = np.load(os.path.join(STATE, "meta.npy"), allow_pickle=True).item()
    periods = meta["PERIODS"]
    tk = np.zeros((H, W), dtype=np.float32)
    p = 0
    while p <= periods:
        x = int(round(ML + (MR - 1 - ML) * p / periods))
        h = 13 if p % 50 == 0 else 7
        tk[TICK_Y:TICK_Y + h, x:x + 1] = 0.45 if p % 50 == 0 else 0.30
        p += 10
    over(page, tk, INK)

    blit(page, text_cov(HEADER, 2), ML, 46, INK, 0.72)
    blit(page, text_cov(FOOT_L, 2), ML, 1040, INK, 0.50)
    fr = text_cov(FOOT_R, 2)
    blit(page, fr, MR - fr.shape[1], 1040, INK, 0.50)

    for k, lab in enumerate(LANE_LABELS):
        c = text_cov(lab)
        y = LANE_TOP + k * LANE_H + (LANE_H - LANE_GAP) - c.shape[0] - 1
        blit(page, c, ML - 14 - c.shape[1], y, INK, 0.60)

    return page


def chain_coverage(d34, xs, cols):
    """Anti-aliased stroke through the chain, one sample per pixel column."""
    y = CY - AMP_PX * np.interp(cols, xs, d34)
    slope = np.gradient(y)
    hw = np.clip(HALF_W * np.sqrt(1.0 + slope * slope), HALF_W, 7.0)
    rows = np.arange(CHAIN_T, CHAIN_B, dtype=np.float32)[:, None]
    cov = np.minimum(y + hw, rows + 1) - np.maximum(y - hw, rows)
    np.clip(cov, 0.0, 1.0, out=cov)
    return cov, y


def stamp_dots(band, d34, xs):
    """Beads. The chain is 32 masses, not a continuum."""
    ys = CY - AMP_PX * d34
    r = DOT_R
    for xi, yi in zip(xs, ys):
        x0, x1 = int(xi - r - 1), int(xi + r + 2)
        y0, y1 = int(yi - r - 1) - CHAIN_T, int(yi + r + 2) - CHAIN_T
        x0, x1 = max(0, x0), min(W, x1)
        y0, y1 = max(0, y0), min(band.shape[0], y1)
        if x1 <= x0 or y1 <= y0:
            continue
        gy = np.arange(y0, y1)[:, None] + CHAIN_T + 0.5 - yi
        gx = np.arange(x0, x1)[None, :] + 0.5 - xi
        d = np.sqrt(gy * gy + gx * gx)
        c = np.clip(r + 0.5 - d, 0.0, 1.0)
        np.maximum(band[y0:y1, x0:x1], c, out=band[y0:y1, x0:x1])


def lane_column(page, x, vals):
    """One pen stroke of the ledger."""
    for k in range(N_LANES):
        top = LANE_TOP + k * LANE_H
        base = top + (LANE_H - LANE_GAP)
        usable = LANE_H - LANE_GAP - 3
        v = float(np.clip(vals[k], 0.0, 1.0))
        top_y = base - v * usable
        rows = np.arange(top, base, dtype=np.float32)
        cov = np.clip(np.minimum(float(base), rows + 1) - np.maximum(top_y, rows), 0.0, 1.0)
        col = page[top:base, x]
        color = ACCENT if k == 0 else INK
        a = (cov * (0.92 if k == 0 else 0.80))[:, None]
        col *= (1.0 - a)
        col += a * color


def main():
    preview = "--preview" in sys.argv
    fps = 30 if preview else int(os.environ.get("FPS", "60"))

    E = np.load(os.path.join(STATE, "energies.npy")).astype(np.float32)
    D = np.load(os.path.join(STATE, "disp.npy"))
    meta = np.load(os.path.join(STATE, "meta.npy"), allow_pickle=True).item()

    control_hz = meta["CONTROL_HZ"]
    dur = meta["PIECE_SECONDS"]
    periods = meta["PERIODS"]
    step = int(round(control_hz / fps))
    n_frames = E.shape[0] // step
    print("fps=%d  frames=%d  control step=%d" % (fps, n_frames, step))

    tot = E.sum(axis=1)
    lanes = np.empty((E.shape[0], N_LANES), dtype=np.float32)
    lanes[:, :5] = E[:, :5] / tot[:, None]
    lanes[:, 5] = E[:, 5:].sum(axis=1) / tot
    print("lane 6+ over the whole run: max %.3f  mean %.3f"
          % (lanes[:, 5].max(), lanes[:, 5].mean()))

    page = build_page()
    xs = np.linspace(ML, MR, 34)
    cols = np.arange(ML, MR + 1)
    band_h = CHAIN_B - CHAIN_T
    trail = np.zeros((band_h, W), dtype=np.float32)
    decay = 0.5 ** (1.0 / (TRAIL_SECONDS * fps))

    if preview:
        os.makedirs(os.path.join(STATE, "preview"), exist_ok=True)
        marks = [0, 8, 40, 150, 300, 406, 445]
    else:
        proc = subprocess.Popen([
            "ffmpeg", "-hide_banner", "-loglevel", "warning", "-y",
            "-f", "rawvideo", "-pix_fmt", "rgb24", "-s", "%dx%d" % (W, H),
            "-r", str(fps), "-i", "-",
            "-f", "f32le", "-ar", "48000", "-ac", "2",
            "-i", os.path.join(STATE, "audio.f32"),
            "-c:v", "libx264", "-preset", "medium", "-crf", "16",
            "-pix_fmt", "yuv420p", "-x264-params", "keyint=120",
            "-c:a", "aac", "-b:a", "256k",
            "-movflags", "+faststart", "-shortest",
            os.path.join(HERE, "tsingou.mp4"),
        ], stdin=subprocess.PIPE)

    n_control = E.shape[0]

    def ci_of_x(xx):
        return int(round((xx - ML) / (MR - 1 - ML) * (n_control - 1)))

    frame = np.empty((H, W, 3), dtype=np.uint8)
    page_u8 = np.empty((H, W, 3), dtype=np.uint8)
    band_base = page[CHAIN_T:CHAIN_B].copy()
    band = np.empty_like(band_base)
    b2 = np.empty((band_h, W), dtype=np.float32)
    d34 = np.zeros(34, dtype=np.float32)
    last_col = ML - 1
    read_x, read_y = MR - glyphs.width("T = 000.0 PERIODS", 2), 46
    page_dirty = True

    import time
    t0 = time.time()
    for f in range(n_frames):
        ci = f * step
        d34[1:33] = D[ci]

        # advance the pen; each column carries the peak of the span it covers
        x = int(ML + (MR - 1 - ML) * ci / (n_control - 1))
        if x > last_col:
            for xx in range(last_col + 1, x + 1):
                a, b = ci_of_x(xx), ci_of_x(xx + 1)
                lane_column(page, xx, lanes[a:max(b, a + 1)].max(axis=0))
            last_col = x
            page_dirty = True

        cov, _ = chain_coverage(d34, xs, cols)
        trail *= decay
        np.maximum(trail[:, ML:MR + 1], cov, out=trail[:, ML:MR + 1])

        if page_dirty:
            page_u8[:] = np.clip(page * 255.0 + 0.5, 0, 255).astype(np.uint8)
            page_dirty = False

        frame[:CHAIN_T] = page_u8[:CHAIN_T]
        frame[CHAIN_B:] = page_u8[CHAIN_B:]

        np.copyto(band, band_base)
        over(band, trail * TRAIL_MAX, INK)
        b2[:] = 0.0
        b2[:, ML:MR + 1] = cov
        stamp_dots(b2, d34, xs)
        over(band, b2, INK)
        frame[CHAIN_T:CHAIN_B] = np.clip(band * 255.0 + 0.5, 0, 255).astype(np.uint8)

        # live readout
        t_per = periods * ci / (n_control - 1)
        rc = text_cov("T = %5.1f PERIODS" % t_per, 2)
        reg = page[read_y:read_y + rc.shape[0], read_x:read_x + rc.shape[1]].copy()
        over(reg, rc * 0.72, INK)
        frame[read_y:read_y + rc.shape[0], read_x:read_x + rc.shape[1]] = \
            np.clip(reg * 255.0 + 0.5, 0, 255).astype(np.uint8)

        if preview:
            sec = ci / control_hz
            if any(abs(sec - m) < 0.5 / fps for m in marks):
                import zlib, struct
                p = os.path.join(STATE, "preview", "t%03d.png" % round(sec))
                write_png(p, frame)
                print("  wrote", p)
        else:
            proc.stdin.write(frame.tobytes())
            if f % 300 == 0:
                el = time.time() - t0
                print("  %5.1f%%  %6.1f fps  eta %5.1f min"
                      % (100.0 * f / n_frames, f / max(el, 1e-9),
                         (n_frames - f) / max(f / max(el, 1e-9), 1e-9) / 60), flush=True)

    if not preview:
        proc.stdin.close()
        proc.wait()
        print("done in %.1f min" % ((time.time() - t0) / 60))


def write_png(path, arr):
    import zlib, struct
    h, w, _ = arr.shape
    raw = b"".join(b"\x00" + arr[y].tobytes() for y in range(h))
    def chunk(t, d):
        c = t + d
        return struct.pack(">I", len(d)) + c + struct.pack(">I", zlib.crc32(c))
    png = (b"\x89PNG\r\n\x1a\n"
           + chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 2, 0, 0, 0))
           + chunk(b"IDAT", zlib.compress(raw, 6))
           + chunk(b"IEND", b""))
    open(path, "wb").write(png)


if __name__ == "__main__":
    main()
