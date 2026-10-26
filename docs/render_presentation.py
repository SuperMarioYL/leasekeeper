#!/usr/bin/env python3
"""Render Leasekeeper's README and website artwork from authored facts.

Every figure is built from docs/presentation.json plus the palette the pinned
renderer produced (web/palette.json), so colour, type scale and motion rhythm
stay consistent and the palette id can be stamped on each root <svg>.

Outputs land in assets/presentation (README) and are copied to
web/assets/presentation (website), because site.json references them from a
web-root-relative path.
"""

import argparse
import json
import math
import shutil
from html import escape
from pathlib import Path

# ---------------------------------------------------------------- utilities


def local(value):
    """Read the English side of a bilingual value."""
    if isinstance(value, dict):
        return str(value.get("en") or value.get("zh") or "")
    return str(value or "")


def wrap(text, width, size):
    """Wrap on spaces to a rough pixel width."""
    limit = max(8, int(width / (size * 0.56)))
    words, lines, line = text.split(), [], ""
    for word in words:
        candidate = f"{line} {word}".strip()
        if len(candidate) > limit and line:
            lines.append(line)
            line = word
        else:
            line = candidate
    if line:
        lines.append(line)
    return lines or [""]


def pseudo(seed, count, lo, hi):
    """Deterministic pseudo-random floats in [lo, hi); no RNG state to drift."""
    out = []
    state = seed
    for _ in range(count):
        state = (1103515245 * state + 12345) % 2147483648
        out.append(lo + (state / 2147483648) * (hi - lo))
    return out


# ------------------------------------------------------------------ palette

CSS = """
text{font-family:"Helvetica Neue",Helvetica,Arial,sans-serif}
.mono{font-family:"SF Mono",ui-monospace,Menlo,Consolas,monospace}
.pulse{animation:pulse 3.6s ease-in-out infinite}
.pulse-b{animation:pulse 3.6s ease-in-out infinite;animation-delay:1.2s}
.pulse-c{animation:pulse 3.6s ease-in-out infinite;animation-delay:2.4s}
@keyframes pulse{0%,100%{opacity:.35}50%{opacity:1}}
.flow{stroke-dasharray:9 131;animation:flow 5.5s linear infinite}
.flow-b{animation-delay:1.8s}
.flow-c{animation-delay:3.7s}
@keyframes flow{from{stroke-dashoffset:140}to{stroke-dashoffset:0}}
.drift{animation:drift 9s ease-in-out infinite}
.drift-b{animation:drift 9s ease-in-out infinite;animation-delay:3s}
@keyframes drift{0%,100%{opacity:.18}50%{opacity:.55}}
.spin{transform-box:view-box;animation:spin 30s linear infinite}
.spin-r{transform-box:view-box;animation:spin 42s linear infinite reverse}
@keyframes spin{to{transform:rotate(360deg)}}
@media (prefers-reduced-motion:reduce){.pulse,.pulse-b,.pulse-c,.flow,.flow-b,.flow-c,.drift,.drift-b,.spin,.spin-r{animation:none}}
"""


def defs(p, seed):
    """Shared gradients, glow and patterns, all coloured from the palette."""
    return (
        f'<linearGradient id="brand" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="1000" y2="430">'
        f'<stop stop-color="{p["primary"]}"/><stop offset=".55" stop-color="{p["secondary"]}"/>'
        f'<stop offset="1" stop-color="{p["highlight"]}"/></linearGradient>'
        f'<radialGradient id="halo"><stop stop-color="{p["primary"]}" stop-opacity=".30"/>'
        f'<stop offset="1" stop-color="{p["bg"]}" stop-opacity="0"/></radialGradient>'
        f'<radialGradient id="halo2"><stop stop-color="{p["highlight"]}" stop-opacity=".22"/>'
        f'<stop offset="1" stop-color="{p["bg"]}" stop-opacity="0"/></radialGradient>'
        f'<filter id="glow" x="-40%" y="-40%" width="180%" height="180%">'
        f'<feGaussianBlur stdDeviation="6" result="b"/><feMerge>'
        f'<feMergeNode in="b"/><feMergeNode in="SourceGraphic"/></feMerge></filter>'
        f'<pattern id="grid" width="34" height="34" patternUnits="userSpaceOnUse">'
        f'<path d="M34 0H0V34" fill="none" stroke="{p["line"]}" stroke-opacity=".5" stroke-width=".6"/></pattern>'
        f'<marker id="arrow" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="5.5" markerHeight="5.5" orient="auto">'
        f'<path d="m1 1 7 4-7 4" fill="none" stroke="{p["primary"]}" stroke-width="1.6"/></marker>'
        f'<marker id="arrow-d" viewBox="0 0 10 10" refX="8" refY="5" markerWidth="5.5" markerHeight="5.5" orient="auto">'
        f'<path d="m1 1 7 4-7 4" fill="none" stroke="{p["muted"]}" stroke-width="1.4"/></marker>'
    )


class Figure:
    def __init__(self, palette, facts):
        self.palette = palette
        self.facts = facts
        self.written = {}

    # ---- primitives -------------------------------------------------
    def text(self, p, x, y, s, size=16, color="ink", anchor="start", weight=400, mono=False):
        cls = ' class="mono"' if mono else ""
        return (
            f'<text{cls} x="{x}" y="{y}" fill="{p[color]}" font-size="{size}" '
            f'text-anchor="{anchor}" font-weight="{weight}">{escape(s)}</text>'
        )

    def paragraph(self, p, x, y, s, width, size=14, color="muted", limit=4, anchor="start"):
        rows = wrap(s, width, size)
        if len(rows) > limit:
            rows = rows[:limit]
            rows[-1] = rows[-1].rstrip(".,") + "…"
        return "".join(
            self.text(p, x, y + i * size * 1.45, row, size, color, anchor)
            for i, row in enumerate(rows)
        )

    def card(self, p, x, y, w, h, title, detail, accent="primary", dash=None):
        stroke = p[accent] if accent else p["line"]
        dash = f' stroke-dasharray="{dash}"' if dash else ""
        body = (
            f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="10" fill="{p["panel"]}" '
            f'stroke="{stroke}" stroke-opacity=".55" stroke-width="1.2"{dash}/>'
        )
        body += f'<rect x="{x}" y="{y}" width="3" height="{h}" rx="1.5" fill="{p[accent]}"/>'
        body += self.text(p, x + 14, y + 26, title, 14.5, "ink", weight=600)
        if detail:
            body += self.paragraph(p, x + 14, y + 46, detail, w - 26, 11.5, "muted", limit=2)
        return body

    def chip(self, p, x, y, w, h, label, color):
        return (
            f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{h / 2}" fill="{p[color]}" '
            f'fill-opacity=".14" stroke="{p[color]}" stroke-opacity=".7" stroke-width="1.1"/>'
            + self.text(p, x + w / 2, y + h / 2 + 5, label, 13, color, anchor="middle", weight=600)
        )

    def panel(self, p, x, y, w, h, title=None):
        body = (
            f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="14" fill="{p["code"]}" '
            f'stroke="{p["line"]}" stroke-width="1.2"/>'
        )
        if title:
            body += f'<rect x="{x}" y="{y}" width="{w}" height="34" rx="14" fill="{p["panel"]}"/>'
            body += f'<rect x="{x}" y="{y + 20}" width="{w}" height="14" fill="{p["panel"]}"/>'
            body += f'<line x1="{x}" y1="{y + 34}" x2="{x + w}" y2="{y + 34}" stroke="{p["line"]}" stroke-width="1"/>'
            for i, color in enumerate(["#f06060", "#e8c34a", "#57c46a"]):
                body += f'<circle cx="{x + 20 + i * 16}" cy="{y + 17}" r="5" fill="{color}" fill-opacity=".85"/>'
            body += self.text(p, x + 74, y + 22, title, 12, "muted", weight=600)
        return body

    # ---- shared scenery --------------------------------------------
    def backdrop(self, p, w, h, halo=True):
        body = f'<rect width="{w}" height="{h}" fill="{p["bg"]}"/>'
        body += f'<rect width="{w}" height="{h}" fill="url(#grid)" opacity=".5"/>'
        if halo:
            body += f'<circle cx="{w * 0.5}" cy="{h * 0.5}" r="{min(w, h) * 0.85}" fill="url(#halo)"/>'
        return body

    def particles(self, p, w, h, seed, count, cx, cy, spread, color="highlight"):
        body = ""
        xs = pseudo(seed, count, 0, w)
        ys = pseudo(seed + 7, count, 0, h)
        rs = pseudo(seed + 13, count, 0.7, 2.4)
        os = pseudo(seed + 19, count, 0.25, 0.8)
        for i in range(count):
            # Pull each dot toward the mark so the field reads as convergence.
            t = 0.62
            x = xs[i] * (1 - t) + cx * t + (xs[i] - cx) * 0.28
            y = ys[i] * (1 - t) + cy * t + (ys[i] - cy) * 0.28
            if math.hypot(x - cx, y - cy) > spread:
                continue
            cls = ["pulse", "pulse-b", "pulse-c"][i % 3]
            body += (
                f'<circle class="{cls}" cx="{x:.1f}" cy="{y:.1f}" r="{rs[i]:.2f}" '
                f'fill="{p[color]}" fill-opacity="{os[i]:.2f}"/>'
            )
        return body

    def mark(self, p, cx, cy, scale=1.0):
        """The lease mark: a gate holding one token, with a second approaching."""
        r = 34 * scale
        body = f'<circle cx="{cx}" cy="{cy}" r="{r * 2.6}" fill="url(#halo2)"/>'
        body += (
            f'<g filter="url(#glow)">'
            f'<circle cx="{cx}" cy="{cy}" r="{r}" fill="none" stroke="{p["primary"]}" stroke-width="2.4"/>'
            f'<path d="M{cx - r * 0.42} {cy - r * 0.62}h{r * 0.84}a{r * 0.42} {r * 0.42} 0 0 1 0 {r * 0.84}'
            f'h-{r * 0.84}a{r * 0.42} {r * 0.42} 0 0 1 0 -{r * 0.84}z" fill="none" '
            f'stroke="{p["highlight"]}" stroke-width="2.2"/>'
            f'<circle cx="{cx}" cy="{cy + r * 0.12}" r="{r * 0.16}" fill="{p["highlight"]}"/>'
            f'<path d="M{cx} {cy + r * 0.26}v{r * 0.34}" stroke="{p["highlight"]}" stroke-width="2.2" stroke-linecap="round"/>'
            f'</g>'
        )
        body += (
            f'<circle class="pulse" cx="{cx + r * 1.9}" cy="{cy - r * 0.9}" r="{r * 0.17}" fill="{p["secondary"]}"/>'
            f'<circle class="pulse-b" cx="{cx - r * 1.75}" cy="{cy + r * 0.95}" r="{r * 0.14}" fill="{p["primary"]}"/>'
        )
        return body

    # ---- compositions ----------------------------------------------
    def hero(self, p, w, h, mobile=False):
        if mobile:
            body = self.backdrop(p, w, h)
            body += self.particles(p, w, h, 91, 70, w / 2, 130, 300)
            body += self.mark(p, w / 2, 118, 0.85)
            body += self.text(p, w / 2, 268, "leasekeeper", 40, "ink", anchor="middle", weight=700)
            body += (
                f'<rect x="{w / 2 - 70}" y="286" width="140" height="3" rx="1.5" fill="url(#brand)"/>'
            )
            body += self.paragraph(p, w / 2, 320, "Stops parallel coding agents from deadlocking.",
                                   w - 60, 16, "muted", limit=3, anchor="middle")
            body += self.chip(p, w / 2 - 118, 396, 236, 34, "acquire  ·  release  ·  refuse", "primary")
            body += self.text(p, w / 2, 470, "one binary · no daemon", 13, "muted", anchor="middle")
            body += self.text(p, w / 2, 512, "file · port · gpu · quota", 13, "muted", anchor="middle")
            return body

        body = self.backdrop(p, w, h)
        body += self.particles(p, w, h, 23, 150, 285, h / 2, 330)
        body += self.mark(p, 285, h / 2 - 6, 1.0)
        body += self.text(p, 520, 150, "leasekeeper", 62, "ink", weight=700)
        body += f'<rect x="522" y="176" width="230" height="4" rx="2" fill="url(#brand)"/>'
        body += self.paragraph(
            p, 522, 220,
            "Stops parallel coding agents from deadlocking. A wait-for graph refuses the circular wait "
            "before it locks up.",
            430, 17, "muted", limit=3,
        )
        body += self.chip(p, 522, 300, 268, 36, "acquire  ·  release  ·  refuse", "primary")
        body += self.text(p, 522, 372, "file · port · gpu · quota", 14, "muted")
        body += self.text(p, 522, 396, "one local binary · no daemon · no database", 14, "muted")
        return body

    def architecture(self, p, w, h, mobile=False):
        nodes = self.facts["architecture"]["nodes"]
        title = self.facts["architecture"]["title"]
        if mobile:
            body = self.backdrop(p, w, h)
            body += self.text(p, 28, 46, title, 17, "ink", weight=600)
            body += self.paragraph(p, 28, 72, "Six packages, one process per run.", w - 56, 12.5, "muted", limit=2)
            y = 108
            for i, node in enumerate(nodes):
                body += self.card(p, 28, y, w - 56, 62, node["title"], node["detail"])
                if i < len(nodes) - 1:
                    body += (
                        f'<path d="M{w / 2} {y + 62}v18" stroke="{p["primary"]}" stroke-opacity=".6" '
                        f'stroke-width="1.6" marker-end="url(#arrow)"/>'
                    )
                y += 80
            return body

        body = self.backdrop(p, w, h)
        body += self.text(p, 40, 46, title, 18, "ink", weight=600)
        body += self.paragraph(p, 40, 72, "The packages a run touches, and the one that owns the state.",
                              360, 12.5, "muted", limit=2)
        body += f'<line x1="40" y1="96" x2="{w - 40}" y2="96" stroke="{p["line"]}" stroke-width="1"/>'

        cw, ch = 196, 74
        top = 128
        xs = [40, 268, 496, 724]
        for i, x in enumerate(xs):
            node = nodes[i]
            body += self.card(p, x, top, cw, ch, node["title"], node["detail"])
            if i < len(xs) - 1:
                body += (
                    f'<path d="M{x + cw + 6} {top + ch / 2}h20" stroke="{p["muted"]}" stroke-opacity=".8" '
                    f'stroke-width="1.6" marker-end="url(#arrow-d)"/>'
                    f'<path class="flow flow-{chr(97 + i)}" d="M{x + cw + 6} {top + ch / 2}h20" '
                    f'stroke="{p["primary"]}" stroke-width="1.8" fill="none"/>'
                )

        branch_y = top + ch + 44
        for i, idx in enumerate([4, 5]):
            node = nodes[idx]
            x = 300 + i * 288
            body += self.card(p, x, branch_y, 264, ch, node["title"], node["detail"])
            start_x = xs[3] + cw / 2
            body += (
                f'<path d="M{start_x} {top + ch}v22h{x + 132 - start_x}v18" fill="none" '
                f'stroke="{p["secondary"]}" stroke-opacity=".75" stroke-width="1.6" marker-end="url(#arrow)"/>'
            )
            body += (
                f'<path class="flow flow-{"b" if i == 0 else "c"}" fill="none" stroke="{p["secondary"]}" '
                f'stroke-width="1.8" d="M{start_x} {top + ch}v22h{x + 132 - start_x}v18"/>'
            )
        body += self.text(p, 40, branch_y + ch + 34,
                          "The broker owns the table and the graph; everything else is a caller.",
                          13, "muted")
        return body

    def process(self, p, w, h, mobile=False):
        steps = self.facts["process"]["steps"]
        title = self.facts["process"]["title"]
        result = self.facts["process"]["result"]
        if mobile:
            body = self.backdrop(p, w, h)
            body += self.text(p, 28, 46, title, 17, "ink", weight=600)
            y = 86
            for i, step in enumerate(steps):
                body += f'<circle cx="52" cy="{y + 22}" r="17" fill="{p["primary"]}" fill-opacity=".16" stroke="{p["primary"]}" stroke-opacity=".7"/>'
                body += self.text(p, 52, y + 27, str(i + 1), 15, "primary", anchor="middle", weight=700)
                body += self.text(p, 84, y + 18, step["title"], 14.5, "ink", weight=600)
                body += self.paragraph(p, 84, y + 38, step["detail"], w - 120, 11.5, "muted", limit=2)
                if i < len(steps) - 1:
                    body += f'<path d="M52 {y + 39}v24" stroke="{p["primary"]}" stroke-opacity=".55" stroke-width="1.6" marker-end="url(#arrow)"/>'
                y += 82
            body += self.chip(p, 28, y + 8, 120, 30, "granted", "secondary")
            body += self.chip(p, 158, y + 8, 120, 30, "queued", "highlight")
            body += self.chip(p, 28, y + 48, 250, 30, "refused: b -> a -> b", "ink")
            body += self.paragraph(p, 28, y + 112, result, w - 56, 12.5, "muted", limit=4)
            return body

        body = self.backdrop(p, w, h)
        body += self.text(p, 40, 46, title, 18, "ink", weight=600)
        body += self.paragraph(p, 40, 72, "Four checks, three outcomes — and a refusal that names the cycle.",
                              560, 12.5, "muted", limit=2)
        body += f'<line x1="40" y1="96" x2="{w - 40}" y2="96" stroke="{p["line"]}" stroke-width="1"/>'

        y = 150
        cw, gap = 202, 34
        for i, step in enumerate(steps):
            x = 40 + i * (cw + gap)
            body += f'<rect x="{x}" y="{y}" width="{cw}" height="112" rx="12" fill="{p["panel"]}" stroke="{p["line"]}" stroke-width="1.1"/>'
            body += f'<circle cx="{x + 30}" cy="{y + 30}" r="16" fill="{p["primary"]}" fill-opacity=".16" stroke="{p["primary"]}" stroke-opacity=".7"/>'
            body += self.text(p, x + 30, y + 35, str(i + 1), 14, "primary", anchor="middle", weight=700)
            body += self.text(p, x + 54, y + 36, step["title"], 13.5, "ink", weight=600)
            body += self.paragraph(p, x + 16, y + 62, step["detail"], cw - 32, 11.5, "muted", limit=3)
            if i < len(steps) - 1:
                body += (
                    f'<path d="M{x + cw + 4} {y + 56}h{gap - 8}" stroke="{p["muted"]}" stroke-opacity=".8" '
                    f'stroke-width="1.6" marker-end="url(#arrow-d)"/>'
                    f'<path class="flow flow-{chr(97 + i)}" d="M{x + cw + 4} {y + 56}h{gap - 8}" '
                    f'stroke="{p["primary"]}" stroke-width="1.8" fill="none"/>'
                )

        vy = y + 152
        body += self.text(p, 40, vy - 12, "Outcomes", 13, "muted", weight=600)
        body += self.chip(p, 40, vy, 150, 34, "granted", "secondary")
        body += self.chip(p, 204, vy, 150, 34, "queued → granted", "highlight")
        body += self.chip(p, 368, vy, 300, 34, "refused: b -> a -> b", "ink")
        body += f'<line x1="40" y1="{vy + 60}" x2="{w - 40}" y2="{vy + 60}" stroke="{p["line"]}" stroke-width="1"/>'
        body += self.paragraph(p, 40, vy + 88, result, w - 80, 13.5, "ink", limit=2)
        return body

    def integrations(self, p, w, h, mobile=False):
        items = self.facts["integrations"]["items"]
        title = self.facts["integrations"]["title"]
        if mobile:
            body = self.backdrop(p, w, h)
            body += self.text(p, 28, 46, title, 16, "ink", weight=600)
            body += self.text(p, 28, 72, "Four resource kinds, one wrapper.", 12.5, "muted")
            body += f'<circle cx="{w / 2}" cy="140" r="44" fill="{p["panel"]}" stroke="{p["primary"]}" stroke-opacity=".7" stroke-width="1.6"/>'
            body += self.text(p, w / 2, 145, "broker", 15, "primary", anchor="middle", weight=700)
            # A left spine with one tick per card keeps six connectors readable
            # at phone width; curves fanned from the hub would cross the cards.
            first_cy = 230 + 28
            last_cy = first_cy + (len(items) - 1) * 64
            body += (
                f'<path d="M{w / 2} 184C{w / 2} 212 44 206 44 {first_cy}" fill="none" '
                f'stroke="{p["secondary"]}" stroke-opacity=".5" stroke-width="1.4"/>'
            )
            body += (
                f'<line x1="44" y1="{first_cy}" x2="44" y2="{last_cy}" stroke="{p["secondary"]}" '
                f'stroke-opacity=".45" stroke-width="1.4"/>'
            )
            y = 230
            for i, item in enumerate(items):
                cy = first_cy + i * 64
                body += (
                    f'<line x1="44" y1="{cy}" x2="64" y2="{cy}" stroke="{p["secondary"]}" '
                    f'stroke-opacity=".45" stroke-width="1.4"/>'
                )
                body += self.card(p, 64, y, w - 92, 56, item["title"], item["detail"])
                y += 64
            return body

        body = self.backdrop(p, w, h)
        body += self.text(p, 40, 46, title, 18, "ink", weight=600)
        body += self.paragraph(p, 40, 72, "The kind is part of the name, so a path can never collide with a port.",
                              420, 12.5, "muted", limit=2)
        body += f'<line x1="40" y1="96" x2="{w - 40}" y2="96" stroke="{p["line"]}" stroke-width="1"/>'

        hub_x, hub_y = 150, 262
        body += f'<circle cx="{hub_x}" cy="{hub_y}" r="118" fill="none" stroke="{p["line"]}" stroke-width="1" stroke-dasharray="4 7"/>'
        body += f'<circle cx="{hub_x}" cy="{hub_y}" r="86" fill="none" stroke="{p["line"]}" stroke-width="1" stroke-dasharray="4 7"/>'
        body += f'<g class="spin" style="transform-origin:{hub_x}px {hub_y}px">'
        body += f'<circle cx="{hub_x + 118}" cy="{hub_y}" r="5" fill="{p["primary"]}"/>'
        body += f'<circle cx="{hub_x - 86}" cy="{hub_y}" r="3.5" fill="{p["secondary"]}"/></g>'
        body += f'<circle cx="{hub_x}" cy="{hub_y}" r="62" fill="{p["panel"]}" stroke="{p["primary"]}" stroke-opacity=".7" stroke-width="1.8"/>'
        body += self.text(p, hub_x, hub_y - 6, "broker", 17, "primary", anchor="middle", weight=700)
        body += self.text(p, hub_x, hub_y + 18, "table + graph", 11.5, "muted", anchor="middle")

        cw, ch = 200, 66
        for i, item in enumerate(items):
            col, row = divmod(i, 3)
            x = 372 + col * 288
            y = 132 + row * 88
            body += self.card(p, x, y, cw, ch, item["title"], item["detail"])
            mid_y = y + ch / 2
            body += (
                f'<path d="M{hub_x + 62} {hub_y}C{hub_x + 150} {hub_y} {x - 90} {mid_y} {x - 6} {mid_y}" '
                f'fill="none" stroke="{p["secondary"]}" stroke-opacity=".38" stroke-width="1.3"/>'
            )
            body += (
                f'<path class="flow flow-{chr(97 + i % 3)}" '
                f'd="M{hub_x + 62} {hub_y}C{hub_x + 150} {hub_y} {x - 90} {mid_y} {x - 6} {mid_y}" '
                f'fill="none" stroke="{p["primary"]}" stroke-width="1.6"/>'
            )
        body += self.text(p, 40, 404, "Reached through `leasekeeper run`, which puts the socket in the child's environment.",
                          12.5, "muted")
        return body

    def scene(self, p, w, h):
        """A static neon graph: the website's fallback, never animated."""
        body = self.backdrop(p, w, h)
        nodes = [
            ("a", 200, 210, "primary"), ("b", 500, 130, "secondary"),
            ("file:src/a.go", 800, 190, "highlight"), ("port:8080", 500, 320, "primary"),
            ("gpu:0", 200, 350, "secondary"), ("quota:api", 800, 340, "highlight"),
        ]
        edges = [(0, 2), (1, 3), (3, 0), (4, 0), (5, 2), (1, 5), (3, 4)]
        for i, j in edges:
            x1, y1 = nodes[i][1], nodes[i][2]
            x2, y2 = nodes[j][1], nodes[j][2]
            body += (f'<path d="M{x1} {y1}L{x2} {y2}" stroke="{p["line"]}" stroke-opacity=".85" stroke-width="1.4"/>')
        body += (f'<path d="M200 210L500 320" stroke="{p["primary"]}" stroke-width="2" fill="none"/>')
        body += (f'<path d="M500 320L200 210" stroke="{p["secondary"]}" stroke-width="2" fill="none"/>')
        body += self.text(p, 352, 274, "a -> b", 14, "primary", weight=600)
        for label, x, y, color in nodes:
            body += f'<circle cx="{x}" cy="{y}" r="9" fill="{p[color]}" fill-opacity=".18" stroke="{p[color]}" stroke-width="1.6"/>'
            body += self.text(p, x, y - 20, label, 13, "ink", anchor="middle", weight=600)
        body += self.text(p, 40, 46, "Leases and a wait-for graph, in one local binary", 18, "ink", weight=600)
        return body

    def demo(self, p, w, h):
        lines = [
            ("$ leasekeeper demo", "muted"),
            ("[a] acquire file:src/a.go    granted", "secondary"),
            ("[b] acquire port:8080        granted", "secondary"),
            ("[a] acquire port:8080        waiting (held by b)", "highlight"),
            ("[b] acquire file:src/a.go    acquire denied — would deadlock: b -> a -> b", "primary"),
            ("Wait-for graph:  a -> b", "ink"),
        ]
        body = self.backdrop(p, w, h, halo=False)
        body += self.panel(p, 40, 40, w - 80, h - 80, "leasekeeper — two agents, one resource pair")
        y = 92
        for line, color in lines:
            size = 13 if len(line) > 66 else 15
            body += self.text(p, 68, y, line, size, color, mono=True)
            y += 40
        return body


def build(facts, palette, out_dir):
    fig = Figure(palette, facts)
    written = []

    def save(kind, w, h, title, description, body, mobile=False):
        stem = f"{kind}-mobile" if mobile else kind
        name = f"{stem}-{mode}.svg"
        svg = (
            f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}" '
            f'data-palette="{palette["id"]}" role="img" '
            f'aria-labelledby="{stem}-{mode}-t {stem}-{mode}-d">'
            f'<title id="{stem}-{mode}-t">{escape(title)}</title>'
            f'<desc id="{stem}-{mode}-d">{escape(description)}</desc>'
            f'<defs>{defs(palette[mode], 0)}</defs><style>{CSS}</style>{body}</svg>'
        )
        (out_dir / name).write_text(svg)
        written.append(name)

    for mode in ("light", "dark"):
        p = palette[mode]
        name = facts["name"]
        tagline = facts["tagline"]

        save("hero", 1000, 430, f"{name} — {tagline}",
             "Leasekeeper's mark, a converging particle field, and the product name with its one-line advantage.",
             fig.hero(p, 1000, 430))
        save("hero", 460, 560, f"{name} — {tagline}",
             "Leasekeeper's mark above the product name and its one-line advantage.",
             fig.hero(p, 460, 560, mobile=True), mobile=True)

        save("architecture", 1000, 430, "Leasekeeper architecture",
             "Six packages in one binary: the CLI entry, the command surface, the socket client, the broker "
             "holding the lease table and the wait-for graph, and the lease and waitgraph primitives.",
             fig.architecture(p, 1000, 430))
        save("architecture", 460, 620, "Leasekeeper architecture",
             "The same six packages stacked as a vertical pipeline.",
             fig.architecture(p, 460, 620, mobile=True), mobile=True)

        save("process", 1000, 430, facts["process"]["title"],
             "Four checks on one acquire and the three outcomes: granted, queued then granted, and refused "
             "with the cycle printed.",
             fig.process(p, 1000, 430))
        save("process", 460, 620, facts["process"]["title"],
             "The same four checks stacked vertically with the three outcomes beneath them.",
             fig.process(p, 460, 620, mobile=True), mobile=True)

        save("integrations", 1000, 430, facts["integrations"]["title"],
             "The broker hub connected to the four leaseable resource kinds and the run wrapper and socket.",
             fig.integrations(p, 1000, 430))
        save("integrations", 460, 620, facts["integrations"]["title"],
             "The broker above the four leaseable resource kinds and the run wrapper and socket.",
             fig.integrations(p, 460, 620, mobile=True), mobile=True)

        save("scene", 1000, 430, "Leasekeeper graph",
             "A static graph of the agents, the resources they hold and the wait edge between them.",
             fig.scene(p, 1000, 430))

        save("demo-0", 1000, 380, "Recorded leasekeeper demo",
             "The recorded demo: agent a takes the file, agent b takes the port, a waits for the port, and "
             "b's request for the file is refused with the cycle b -> a -> b.",
             fig.demo(p, 1000, 380))
    return written


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo", type=Path, default=Path(__file__).resolve().parent.parent)
    args = parser.parse_args()
    repo = args.repo.resolve()

    facts = json.loads((repo / "docs/presentation.json").read_text())
    palette = json.loads((repo / "web/palette.json").read_text())

    assets = repo / "assets/presentation"
    web = repo / "web/assets/presentation"
    assets.mkdir(parents=True, exist_ok=True)
    web.mkdir(parents=True, exist_ok=True)

    written = build(facts, palette, assets)
    for name in written:
        shutil.copy2(assets / name, web / name)
    print(f"palette {palette['id']}: wrote {len(written)} SVGs to assets/presentation and web/assets/presentation")


if __name__ == "__main__":
    main()
