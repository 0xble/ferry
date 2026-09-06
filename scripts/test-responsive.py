#!/usr/bin/env python3
"""Preview geometry regression. Requires: pip install playwright; playwright install chromium webkit."""
import subprocess
import tempfile
from pathlib import Path

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory() as tmp:
    source = Path(tmp) / "render.go"
    source.write_text('''package main
import ("fmt"; "github.com/0xble/ferry/share")
func main() { fmt.Print(share.RenderHTMLPreviewPage("A long responsive preview title.html", "/r/test", nil)) }
''')
    html = subprocess.check_output(["go", "run", str(source)], cwd=ROOT, text=True)

with sync_playwright() as p:
    count = 0
    for engine in ("webkit", "chromium"):
        browser = getattr(p, engine).launch()
        for width, height in ((320, 568), (375, 812), (507, 768), (768, 1024), (1024, 768), (1366, 1024), (1440, 900)):
            page = browser.new_page(viewport={"width": width, "height": height}, screen={"width": width, "height": height})
            # Model iOS's feature gate on desktop engines. Do not mock layout.
            page.add_init_script('''Object.defineProperty(navigator,"maxTouchPoints",{get:()=>5});
const supports=CSS.supports.bind(CSS);
CSS.supports=(...args)=>args[0]==="-webkit-touch-callout"?true:supports(...args);''')
            page.route("http://ferry.test/**", lambda route: route.fulfill(content_type="text/html", body=html if "/s/" in route.request.url else '<!doctype html><meta name="viewport" content="width=device-width"><body style="margin:0"><h1>Responsive preview</h1><p>Content uses the available width.</p></body>'))
            page.goto("http://ferry.test/s/test")
            def expect_width(expected):
                actual = page.locator("iframe").bounding_box()["width"]
                assert abs(actual - expected) <= 1, (engine, width, height, expected, actual)
                assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), "outer overflow"
            expect_width(width)
            # Split-view and rotation must follow window geometry, not physical screen.
            page.set_viewport_size({"width": 507, "height": 768})
            expect_width(507)
            page.set_viewport_size({"width": 1024, "height": 768})
            expect_width(1024)
            # Embedded browser: visible area narrower than its layout viewport.
            page.evaluate('''Object.defineProperty(visualViewport,"width",{configurable:true,get:()=>600});
visualViewport.dispatchEvent(new Event("resize"));''')
            expect_width(600)
            # Safari shrink-to-fit exposes a widened viewport at scale below one.
            page.evaluate('''Object.defineProperty(visualViewport,"scale",{configurable:true,get:()=>0.5});
Object.defineProperty(visualViewport,"width",{configurable:true,get:()=>1200});
visualViewport.dispatchEvent(new Event("resize"));''')
            expect_width(600)
            # Pinch zoom must magnify rather than trigger layout reflow.
            page.evaluate('''Object.defineProperty(visualViewport,"scale",{configurable:true,get:()=>2});
Object.defineProperty(visualViewport,"width",{configurable:true,get:()=>300});
visualViewport.dispatchEvent(new Event("resize"));''')
            expect_width(600)
            page.evaluate('''Object.defineProperty(visualViewport,"scale",{configurable:true,get:()=>1});
Object.defineProperty(visualViewport,"width",{configurable:true,get:()=>1024});
visualViewport.dispatchEvent(new Event("resize"));''')
            expect_width(1024)
            if engine == "webkit" and width == 1024:
                page.screenshot(path="/tmp/ferry-responsive-landscape.png")
            page.close()
            count += 1
        browser.close()
    print(f"PASS: {count} browser/size cases, including resize, split view, embedded viewport and pinch zoom")
