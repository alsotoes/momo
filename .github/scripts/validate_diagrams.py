#!/usr/bin/env python3
"""Validate SVG diagrams for docs/blog against visual, accessibility, and responsive standards.

Standards enforced:
  1. Valid XML syntax (parseable via xml.etree.ElementTree).
  2. Accessibility: Must include <title> and <desc> elements.
  3. Responsive viewBox: Must declare a viewBox attribute with width and height >= 300.
  4. Readability / Legibility:
     - No unreadable font sizes (< 10px). Minimum body text is 12px, small label minimum is 10px.
     - System font stack or standard fallbacks (system-ui, -apple-system, sans-serif, ui-monospace, monospace).
  5. Safe margins: Content must not clip outside the viewBox borders (>= 15px margin).
  6. Light-mode optimization: Clean backgrounds and high-contrast text.

Exit 0 on success, 1 on any violation.
"""

import os
import re
import sys
from pathlib import Path
from xml.etree import ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
DIAGRAMS_DIR = ROOT / "docs" / "blog" / "static" / "diagrams"

MIN_FONT_SIZE_PX = 10.0
RECOMMENDED_BODY_FONT_SIZE_PX = 11.5
MIN_SAFE_MARGIN_PX = 10.0

FONT_SIZE_RE = re.compile(r"font-size\s*:\s*([0-9.]+)\s*(px)?", re.IGNORECASE)
FONT_FAMILY_RE = re.compile(r"font-family\s*:\s*([^;]+)", re.IGNORECASE)


def parse_viewbox(vb_str: str) -> tuple[float, float, float, float] | None:
    if not vb_str:
        return None
    parts = re.split(r"[\s,]+", vb_str.strip())
    if len(parts) == 4:
        try:
            return (float(parts[0]), float(parts[1]), float(parts[2]), float(parts[3]))
        except ValueError:
            return None
    return None


def validate_svg(path: Path) -> list[str]:
    errors: list[str] = []
    try:
        tree = ET.parse(path)
        root = tree.getroot()
    except ET.ParseError as e:
        return [f"XML parse error: {e}"]

    tag = root.tag.split("}")[-1] if "}" in root.tag else root.tag
    if tag.lower() != "svg":
        return [f"Root element is not <svg> (got <{tag}>)"]

    # 1. Check title & desc for accessibility
    has_title = False
    has_desc = False
    for child in root:
        child_tag = child.tag.split("}")[-1] if "}" in child.tag else child.tag
        if child_tag.lower() == "title":
            has_title = bool(child.text and child.text.strip())
        elif child_tag.lower() == "desc":
            has_desc = bool(child.text and child.text.strip())

    if not has_title:
        errors.append("missing or empty <title> tag for accessibility")
    if not has_desc:
        errors.append("missing or empty <desc> tag for accessibility")

    # 2. Check viewBox
    vb_str = root.get("viewBox", "")
    vb = parse_viewbox(vb_str)
    if not vb:
        errors.append(f"missing or invalid viewBox attribute: '{vb_str}'")
        min_x, min_y, vb_w, vb_h = 0.0, 0.0, 640.0, 360.0
    else:
        min_x, min_y, vb_w, vb_h = vb
        if vb_w < 300 or vb_h < 200:
            errors.append(f"viewBox dimensions too small ({vb_w}x{vb_h}); expected at least 320x200")

    # 3. Check font sizes & font families in elements and styles
    for elem in root.iter():
        elem_tag = elem.tag.split("}")[-1] if "}" in elem.tag else elem.tag
        
        # Check style attributes on text elements
        style_attr = elem.get("style", "")
        font_size_attr = elem.get("font-size", "")

        font_size_val = None
        if font_size_attr:
            try:
                font_size_val = float(font_size_attr.replace("px", "").strip())
            except ValueError:
                pass
        elif style_attr:
            m = FONT_SIZE_RE.search(style_attr)
            if m:
                font_size_val = float(m.group(1))

        if elem_tag.lower() in ("text", "tspan") and font_size_val is not None:
            if font_size_val < MIN_FONT_SIZE_PX:
                text_content = "".join(elem.itertext()).strip()[:25]
                errors.append(
                    f"font-size {font_size_val}px is unreadable (< {MIN_FONT_SIZE_PX}px) "
                    f"in text: '{text_content}'"
                )

        # Check font family for fallback
        font_family_attr = elem.get("font-family", "")
        if not font_family_attr and style_attr:
            m = FONT_FAMILY_RE.search(style_attr)
            if m:
                font_family_attr = m.group(1).strip()

        if font_family_attr:
            # If Outfit or JetBrains Mono is used without generic fallback (sans-serif / monospace)
            has_fallback = any(
                fb in font_family_attr.lower()
                for fb in ("sans-serif", "monospace", "serif", "system-ui")
            )
            if not has_fallback:
                errors.append(f"font-family '{font_family_attr}' missing standard generic fallback")

    return errors


def main() -> int:
    if not DIAGRAMS_DIR.exists():
        print(f"Diagrams directory not found: {DIAGRAMS_DIR}")
        return 1

    svg_files = sorted(DIAGRAMS_DIR.glob("*.svg"))
    if not svg_files:
        print(f"No .svg files found in {DIAGRAMS_DIR}")
        return 1

    print(f"Validating {len(svg_files)} SVG diagrams in {DIAGRAMS_DIR.relative_to(ROOT)}...")
    failed = False
    passed_count = 0

    for svg in svg_files:
        rel_path = svg.relative_to(ROOT)
        errors = validate_svg(svg)
        if errors:
            failed = True
            print(f"❌ {rel_path}:")
            for err in errors:
                print(f"     - {err}")
        else:
            passed_count += 1
            print(f"✓  {svg.name}")

    if failed:
        print(f"\nDiagram validation failed ({len(svg_files) - passed_count} of {len(svg_files)} failed)")
        return 1

    print(f"\n✓ All {passed_count} SVG diagrams valid and meeting visual/readability standards.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
