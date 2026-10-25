"""Render the GhostKey architecture animation (dark mode) as a GIF."""
import sys
from PIL import Image, ImageDraw, ImageFont

OUT = sys.argv[1]
W, H, S = 1280, 720, 2  # S = supersampling factor

BG = (13, 17, 23)
PANEL = (22, 27, 34)
PANEL2 = (28, 34, 43)
BORDER = (48, 54, 61)
TEXT = (230, 237, 243)
MUTED = (139, 148, 158)
BLUE = (88, 166, 255)
PURPLE = (188, 140, 255)
GREEN = (63, 185, 80)
RED = (248, 81, 73)
ORANGE = (240, 136, 62)
YELLOW = (210, 153, 34)
CYAN = (57, 197, 207)

FD = "C:/Windows/Fonts/"


def font(name, size):
    return ImageFont.truetype(FD + name, size * S)


F_TITLE = font("segoeuib.ttf", 24)
F_SUB = font("segoeui.ttf", 15)
F_H = font("segoeuib.ttf", 15)
F_B = font("segoeui.ttf", 13)
F_SMALL = font("segoeui.ttf", 12)
F_MONO = font("consola.ttf", 14)
F_MONO_S = font("consola.ttf", 12)
F_NUM = font("segoeuib.ttf", 13)
F_CAP = font("segoeui.ttf", 17)
F_CAPB = font("segoeuib.ttf", 17)


def s(v):
    return [int(round(x * S)) for x in v]


def mix(a, b, t):
    return tuple(int(a[i] + (b[i] - a[i]) * t) for i in range(3))


def box(d, xy, fill=PANEL, outline=BORDER, width=1.5, r=12):
    d.rounded_rectangle(s(xy), radius=r * S, fill=fill, outline=outline, width=int(width * S))


def text(d, xy, t, f, fill=TEXT, anchor="la"):
    d.text(s(xy), t, font=f, fill=fill, anchor=anchor)


def line(d, pts, fill=BORDER, width=2, dash=None, phase=0.0):
    if not dash:
        d.line([tuple(s(p)) for p in pts], fill=fill, width=int(width * S), joint="curve")
        return
    on, off = dash
    period = on + off
    for (x0, y0), (x1, y1) in zip(pts, pts[1:]):
        L = ((x1 - x0) ** 2 + (y1 - y0) ** 2) ** 0.5
        if L == 0:
            continue
        pos = -((phase * period) % period)
        while pos < L:
            a, b = max(pos, 0), min(pos + on, L)
            if b > a:
                d.line(s((x0 + (x1 - x0) * a / L, y0 + (y1 - y0) * a / L,
                          x0 + (x1 - x0) * b / L, y0 + (y1 - y0) * b / L)),
                       fill=fill, width=int(width * S))
            pos += period


def arrow_head(d, tip, direction, fill, size=7):
    x, y = tip
    dx, dy = direction
    L = (dx * dx + dy * dy) ** 0.5
    dx, dy = dx / L, dy / L
    px, py = -dy, dx
    pts = [(x, y), (x - dx * size * 1.6 + px * size, y - dy * size * 1.6 + py * size),
           (x - dx * size * 1.6 - px * size, y - dy * size * 1.6 - py * size)]
    d.polygon([tuple(s(p)) for p in pts], fill=fill)


def arrow(d, pts, fill=BORDER, width=2, dash=None, phase=0.0):
    line(d, pts, fill, width, dash, phase)
    (x0, y0), (x1, y1) = pts[-2], pts[-1]
    arrow_head(d, pts[-1], (x1 - x0, y1 - y0), fill)


def along(pts, t):
    segs = list(zip(pts, pts[1:]))
    lens = [((b[0] - a[0]) ** 2 + (b[1] - a[1]) ** 2) ** 0.5 for a, b in segs]
    total = sum(lens)
    dist = max(0.0, min(1.0, t)) * total
    for (a, b), L in zip(segs, lens):
        if dist <= L:
            k = dist / L if L else 0
            return a[0] + (b[0] - a[0]) * k, a[1] + (b[1] - a[1]) * k
        dist -= L
    return pts[-1]


def pill(d, center, label, color, f=F_MONO_S):
    x, y = center
    l, t, r, b = d.textbbox((0, 0), label, font=f)
    w = (r - l) / S + 22
    h = 26
    d.rounded_rectangle(s((x - w / 2, y - h / 2, x + w / 2, y + h / 2)), radius=13 * S,
                        fill=mix(BG, color, 0.25), outline=color, width=int(1.5 * S))
    text(d, (x, y), label, f, fill=TEXT, anchor="mm")


def glow(d, xy, color, k):
    if k <= 0:
        return
    x0, y0, x1, y1 = xy
    for i, a in enumerate((0.10, 0.18, 0.30)):
        g = 6 - i * 2
        d.rounded_rectangle(s((x0 - g, y0 - g, x1 + g, y1 + g)), radius=(14 + g) * S,
                            outline=mix(BG, color, a * k * 2.2), width=int(2 * S))


# ---- layout -------------------------------------------------------------
CP = (40, 78, 420, 262)
SECRET = (60, 112, 400, 156)
CTRL = (60, 172, 224, 246)
HOOK = (236, 172, 400, 246)

POD = (460, 78, 860, 262)
APP = (480, 112, 840, 178)
TLS = (480, 192, 840, 246)

KERN = (40, 300, 860, 560)
NODES = [
    ((60, 352, 240, 462), "1", "uprobe", "SSL_write · Go Write", "finds kl:: in plaintext"),
    ((260, 352, 440, 462), "2", "xor path", "host · port · pod binding", "stages shadow XOR real"),
    ((460, 352, 640, 462), "3", "tcp_sendmsg", "SNI check · GHASH key H", "moves patch to tc_pending"),
    ((660, 352, 840, 462), "4", "tc · host veth", "XOR the ciphertext", "recompute GCM tag"),
]
NODE_COLORS = [BLUE, PURPLE, CYAN, GREEN]

STRIPE = (920, 300, 1240, 410)
EVIL = (920, 450, 1240, 560)
BADGE = (920, 78, 1240, 262)

P_MAIN = [(660, 246), (660, 288), (150, 288), (150, 352)]
P_12 = [(240, 407), (260, 407)]
P_23 = [(440, 407), (460, 407)]
P_34 = [(640, 407), (660, 407)]
P_OUT_OK = [(840, 407), (880, 407), (880, 355), (920, 355)]
P_OUT_BAD = [(840, 430), (870, 430), (870, 505), (920, 505)]

CAPTIONS = [
    ("0", "Control plane", "The controller creates a shadow secret (kl:: + random tail, same length); the webhook mounts it in the pod."),
    ("1", "TLS write", "The app sends its placeholder over TLS. An eBPF uprobe reads the plaintext. It never writes to it."),
    ("2", "Policy", "Host, port and per-pod binding all match, so shadow XOR real is staged in a BPF map."),
    ("3", "Socket check", "tcp_sendmsg confirms the ClientHello SNI is api.stripe.com and obtains the GHASH key H."),
    ("4", "Patch", "After encryption, tc on the host veth XORs the AES-GCM ciphertext and recomputes the tag."),
    ("5", "Delivered", "api.stripe.com decrypts the real secret. It never existed in the pod's memory."),
    ("6", "Exfiltration blocked", "The same placeholder sent to another host fails the policy: nothing is staged."),
    ("7", "Blocked", "evil.example only receives the worthless kl:: placeholder."),
]


def draw_static(d, st):
    """st: dict of highlight levels / state."""
    text(d, (40, 22), "GhostKey", F_TITLE, TEXT)
    text(d, (160, 30), "real secrets exist only on the wire, patched in-kernel with eBPF", F_SUB, MUTED)

    # control plane
    box(d, CP, fill=PANEL)
    text(d, (CP[0] + 16, CP[1] + 10), "CONTROL PLANE", F_SMALL, MUTED)
    glow(d, SECRET, YELLOW, st.get("cp", 0))
    box(d, SECRET, fill=PANEL2, outline=mix(BORDER, YELLOW, st.get("cp", 0)))
    text(d, (SECRET[0] + 14, SECRET[1] + 22), "Secret", F_H, TEXT, "lm")
    text(d, (SECRET[2] - 14, SECRET[1] + 22), "sk-live-xyz123", F_MONO, YELLOW, "rm")
    for b, t1, t2 in ((CTRL, "Controller", "DaemonSet · BPF maps"), (HOOK, "Webhook", "Deployment · rewrite")):
        glow(d, b, BLUE, st.get("cp", 0))
        box(d, b, fill=PANEL2, outline=mix(BORDER, BLUE, st.get("cp", 0)))
        text(d, ((b[0] + b[2]) / 2, b[1] + 26), t1, F_H, TEXT, "mm")
        text(d, ((b[0] + b[2]) / 2, b[1] + 50), t2, F_SMALL, MUTED, "mm")

    # pod
    box(d, POD, fill=PANEL)
    text(d, (POD[0] + 16, POD[1] + 10), "APPLICATION POD", F_SMALL, MUTED)
    glow(d, APP, ORANGE, st.get("app", 0))
    box(d, APP, fill=PANEL2, outline=mix(BORDER, ORANGE, st.get("app", 0)))
    text(d, (APP[0] + 14, APP[1] + 20), "app memory (mounted shadow secret)", F_SMALL, MUTED, "lm")
    text(d, (APP[0] + 14, APP[1] + 44), "Authorization: Bearer ", F_MONO, TEXT, "lm")
    text(d, (APP[0] + 190, APP[1] + 44), "kl::q7XmB2rTa9", F_MONO, ORANGE, "lm")
    box(d, TLS, fill=PANEL2)
    text(d, ((TLS[0] + TLS[2]) / 2, TLS[1] + 27), "TLS library  ·  OpenSSL · BoringSSL · Go · Bun", F_B, MUTED, "mm")

    # webhook -> pod, controller -> kernel
    ph = st.get("phase", 0)
    c = mix(BORDER, BLUE, st.get("cp", 0))
    arrow(d, [(400, 209), (430, 209), (430, 145), (480, 145)], c, 2, (6, 5), ph)
    arrow(d, [(80, 246), (80, 352)], c, 2, (6, 5), ph)
    text(d, (88, 270), "sync maps", F_SMALL, mix(MUTED, BLUE, st.get("cp", 0)))

    # kernel band
    box(d, KERN, fill=(17, 22, 29))
    text(d, (KERN[2] - 16, KERN[1] + 18), "LINUX KERNEL · eBPF (node)", F_SMALL, MUTED, "rm")
    for i, (b, n, title, l1, l2) in enumerate(NODES):
        col = NODE_COLORS[i]
        k = st.get("n%d" % i, 0)
        glow(d, b, col, k)
        box(d, b, fill=mix(PANEL2, col, 0.10 * k), outline=mix(BORDER, col, max(k, 0.35)))
        d.ellipse(s((b[0] + 12, b[1] + 12, b[0] + 34, b[1] + 34)), fill=mix(BG, col, 0.85))
        text(d, (b[0] + 23, b[1] + 23), n, F_NUM, BG, "mm")
        text(d, (b[0] + 44, b[1] + 23), title, F_H, TEXT, "lm")
        text(d, (b[0] + 14, b[1] + 60), l1, F_SMALL, MUTED, "lm")
        text(d, (b[0] + 14, b[1] + 84), l2, F_SMALL, mix(MUTED, col, 0.6), "lm")
    for p in (P_12, P_23, P_34):
        arrow(d, p, BORDER, 2)
    text(d, (60, 492), "secret_map  ·  secret_acl  ·  xor_pending  ·  sni_state  ·  tc_pending", F_MONO_S, (110, 118, 129))
    text(d, (60, 520), "No bpf_probe_write_user: user memory is never modified.", F_SMALL, MUTED)
    arrow(d, P_MAIN, mix(BORDER, ORANGE, st.get("app", 0) * 0.8), 2)

    # destinations
    ok = st.get("ok", 0)
    glow(d, STRIPE, GREEN, ok)
    box(d, STRIPE, fill=mix(PANEL, GREEN, 0.08 * ok), outline=mix(BORDER, GREEN, max(ok, 0.35)))
    text(d, (STRIPE[0] + 16, STRIPE[1] + 24), "api.stripe.com", F_H, TEXT, "lm")
    text(d, (STRIPE[2] - 16, STRIPE[1] + 24), "allowed host", F_SMALL, GREEN, "rm")
    text(d, (STRIPE[0] + 16, STRIPE[1] + 58), "decrypts:", F_SMALL, MUTED, "lm")
    text(d, (STRIPE[0] + 16, STRIPE[1] + 84), "Bearer " + ("sk-live-xyz123" if ok > 0.5 else "..."), F_MONO,
         GREEN if ok > 0.5 else MUTED, "lm")

    bad = st.get("bad", 0)
    glow(d, EVIL, RED, bad)
    box(d, EVIL, fill=mix(PANEL, RED, 0.08 * bad), outline=mix(BORDER, RED, max(bad, 0.35)))
    text(d, (EVIL[0] + 16, EVIL[1] + 24), "evil.example", F_H, TEXT, "lm")
    text(d, (EVIL[2] - 16, EVIL[1] + 24), "not allowed", F_SMALL, RED, "rm")
    text(d, (EVIL[0] + 16, EVIL[1] + 58), "receives:", F_SMALL, MUTED, "lm")
    text(d, (EVIL[0] + 16, EVIL[1] + 84), "Bearer " + ("kl::q7XmB2rTa9" if bad > 0.5 else "..."), F_MONO,
         ORANGE if bad > 0.5 else MUTED, "lm")
    arrow(d, P_OUT_OK, mix(BORDER, GREEN, ok), 2)
    arrow(d, P_OUT_BAD, mix(BORDER, RED, bad), 2, (6, 5))

    # badge
    box(d, BADGE, fill=PANEL)
    text(d, (BADGE[0] + 16, BADGE[1] + 10), "GUARANTEES", F_SMALL, MUTED)
    rows = [(GREEN, "Real secret never enters the pod"), (GREEN, "No sidecar, proxy or code change"),
            (GREEN, "Per-pod binding + host/port/SNI"), (GREEN, "AES-128/256-GCM · TLS 1.2 / 1.3")]
    for i, (col, t) in enumerate(rows):
        y = BADGE[1] + 48 + i * 38
        d.ellipse(s((BADGE[0] + 18, y - 7, BADGE[0] + 32, y + 7)), fill=mix(BG, col, 0.85))
        text(d, (BADGE[0] + 44, y), t, F_B, TEXT, "lm")


def draw_caption(d, idx, k):
    box(d, (40, 590, 1240, 690), fill=PANEL)
    n, title, body = CAPTIONS[idx]
    col = [YELLOW, ORANGE, PURPLE, CYAN, GREEN, GREEN, RED, RED][idx]
    d.ellipse(s((62, 618, 98, 654)), fill=mix(PANEL, col, k))
    text(d, (80, 636), n, F_CAPB, BG, "mm")
    text(d, (116, 622), title, F_CAPB, mix(PANEL, col, k), "lm")
    text(d, (116, 652), body, F_CAP, mix(PANEL, TEXT, k), "lm")
    # progress dots
    for i in range(len(CAPTIONS)):
        x = 1100 + i * 16
        d.ellipse(s((x, 618, x + 8, 626)), fill=col if i == idx else BORDER)


def render(st, cap, cap_k, packet=None):
    img = Image.new("RGB", (W * S, H * S), BG)
    d = ImageDraw.Draw(img)
    draw_static(d, st)
    draw_caption(d, cap, cap_k)
    if packet:
        pos, label, col = packet
        pill(d, pos, label, col)
    return img.resize((W, H), Image.LANCZOS)


def ease(t):
    t = max(0.0, min(1.0, t))
    return t * t * (3 - 2 * t)


frames, durs = [], []


def add(img, ms=70):
    frames.append(img)
    durs.append(ms)


def scene(n, cap, fn, ms=70):
    for i in range(n):
        t = i / max(1, n - 1)
        st, packet = fn(t, i)
        add(render(st, cap, 1.0, packet), ms)


# 0 control plane
scene(18, 0, lambda t, i: ({"cp": ease(t * 2), "phase": i * 0.25}, None))
scene(6, 0, lambda t, i: ({"cp": 1, "phase": (18 + i) * 0.25}, None), 140)

# 1 app -> uprobe
scene(18, 1, lambda t, i: ({"app": 1, "n0": ease((t - 0.7) / 0.3)},
                           (along(P_MAIN, ease(t)), "kl::q7XmB2rTa9", ORANGE)))
scene(5, 1, lambda t, i: ({"app": 1, "n0": 1}, ((150, 330), "kl::q7XmB2rTa9", ORANGE)), 140)

# 2 xor path
scene(14, 2, lambda t, i: ({"app": 0.4, "n0": 1 - t * 0.6, "n1": ease(t * 1.5)},
                           ((150 + 200 * ease(t), 330), "shadow XOR real", PURPLE)))
scene(5, 2, lambda t, i: ({"app": 0.4, "n0": 0.4, "n1": 1}, ((350, 330), "shadow XOR real", PURPLE)), 140)

# 3 tcp_sendmsg
scene(14, 3, lambda t, i: ({"n0": 0.4, "n1": 1 - t * 0.6, "n2": ease(t * 1.5)},
                           ((350 + 200 * ease(t), 330), "SNI ok · H", CYAN)))
scene(5, 3, lambda t, i: ({"n0": 0.4, "n1": 0.4, "n2": 1}, ((550, 330), "SNI ok · H", CYAN)), 140)

# 4 tc patch: ciphertext morphs
HEX_A, HEX_B = "9f3a 71c2 e80d ..", "4be1 0d97 23aa .."
scene(16, 4, lambda t, i: ({"n0": 0.4, "n1": 0.4, "n2": 1 - t * 0.6, "n3": ease(t * 1.5)},
                           ((550 + 200 * ease(t * 1.3), 330), HEX_A if t < 0.6 else HEX_B,
                            CYAN if t < 0.6 else GREEN)))
scene(5, 4, lambda t, i: ({"n0": 0.4, "n1": 0.4, "n2": 0.4, "n3": 1}, ((750, 330), HEX_B + " +tag", GREEN)), 140)

# 5 deliver
scene(14, 5, lambda t, i: ({"n0": 0.4, "n1": 0.4, "n2": 0.4, "n3": 1, "ok": ease((t - 0.75) / 0.25)},
                           (along(P_OUT_OK, ease(t)), HEX_B, GREEN)))
scene(14, 5, lambda t, i: ({"n3": 0.5, "ok": 1}, None), 140)

# 6 blocked path
scene(16, 6, lambda t, i: ({"app": 1, "ok": 0.4, "n1": ease((t - 0.6) / 0.4)},
                           (along(P_MAIN + [(150, 330), (350, 330)], ease(t)), "kl::q7XmB2rTa9", ORANGE)))
scene(8, 6, lambda t, i: ({"app": 0.4, "ok": 0.4, "n1": 1}, ((350, 330), "policy: no match", RED)), 140)

# 7 placeholder reaches evil.example unchanged
scene(16, 7, lambda t, i: ({"ok": 0.4, "n1": 0.5, "bad": ease((t - 0.8) / 0.2)},
                           (along([(350, 330), (750, 330), (840, 430)] + P_OUT_BAD[1:], ease(t)),
                            "kl::q7XmB2rTa9", ORANGE)))
scene(20, 7, lambda t, i: ({"ok": 1, "bad": 1}, None), 160)

# shared palette for stable colors across frames
sample = Image.new("RGB", (W, H * 3))
picks = frames[::12]
sample = Image.new("RGB", (W, H * len(picks)))
for k, f in enumerate(picks):
    sample.paste(f, (0, H * k))
pal = sample.quantize(colors=255, method=Image.Quantize.MEDIANCUT)
q = [f.quantize(palette=pal, dither=Image.Dither.NONE) for f in frames]
q[0].save(OUT, save_all=True, append_images=q[1:], duration=durs, loop=0, optimize=True, disposal=1)
print(len(frames), "frames")
