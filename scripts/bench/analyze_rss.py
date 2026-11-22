#!/usr/bin/env python3
"""ER-901a：分析 soak RSS 采样，输出首尾中位数/峰值/斜率。"""
import json
import statistics
import sys
from pathlib import Path


def main() -> int:
    path = Path(sys.argv[1] if len(sys.argv) > 1 else "docs/impl/evidence/ER-901a/rss.log")
    samples = []
    for line in path.read_text().splitlines():
        parts = line.split()
        if len(parts) != 2:
            continue
        try:
            samples.append((int(parts[0]), int(parts[1])))
        except ValueError:
            continue
    if len(samples) < 4:
        print(json.dumps({"error": "not enough samples", "n": len(samples)}))
        return 1
    times = [t - samples[0][0] for t, _ in samples]
    rss = [r for _, r in samples]
    w = max(1, len(rss) // 10)
    first = statistics.median(rss[:w])
    last = statistics.median(rss[-w:])
    n = len(times)
    mean_t = sum(times) / n
    mean_r = sum(rss) / n
    denom = sum((t - mean_t) ** 2 for t in times) or 1
    slope_kb_per_min = (sum((t - mean_t) * (r - mean_r) for t, r in zip(times, rss)) / denom) * 60
    out = {
        "samples": n,
        "duration_s": times[-1],
        "first_median_kb": round(first),
        "last_median_kb": round(last),
        "peak_kb": max(rss),
        "delta_kb": round(last - first),
        "delta_pct": round((last - first) / first * 100, 2) if first else None,
        "slope_kb_per_min": round(slope_kb_per_min, 2),
    }
    print(json.dumps(out))
    bad = (out["delta_pct"] or 0) > 20 or slope_kb_per_min > 1024
    return 2 if bad else 0


if __name__ == "__main__":
    raise SystemExit(main())
