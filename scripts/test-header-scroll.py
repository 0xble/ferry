#!/usr/bin/env python3
"""HTML preview header scroll regression. Requires: pip install playwright; playwright install chromium webkit."""
import json
import subprocess
from pathlib import Path

from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[1]
LONG = "<!doctype html><meta name=\"viewport\" content=\"width=device-width\"><body style=\"margin:0\"><nav id=\"pin\" style=\"position:fixed;right:0;top:var(--ferry-inset-visible,0px);width:40px;height:40px\"></nav><h1 id=\"first\" style=\"margin:0\">Top</h1>" + "<p>Line</p>" * 300 + "</body>"
CLIPPED = "<!doctype html><style>html,body{height:100%;overflow:hidden;margin:0}</style><nav id=\"pin\" style=\"position:fixed;right:0;top:var(--ferry-inset-visible,0px);width:40px;height:40px\"></nav><main style=\"height:100%;overflow:auto\"><h1 id=\"first\" style=\"margin:0\">Top</h1>" + "<p>Line</p>" * 300 + "</main>"

PROBE = f'''package share
import ("encoding/json"; "fmt"; "testing")
func TestHeaderScrollProbe(t *testing.T) {{
 artifacts := map[string]string{{"long": {json.dumps(LONG)}, "clipped": {json.dumps(CLIPPED)}}}
 out := map[string]string{{"preview": RenderHTMLPreviewPage("A long preview title.html", "/r/test", nil)}}
 for name, body := range artifacts {{ out[name] = htmlViewportContainmentMarkup + body }}
 data, _ := json.Marshal(out)
 fmt.Print(string(data))
}}
'''
probe = ROOT / "share" / "zz_header_scroll_probe_test.go"
probe.write_text(PROBE)
try:
    output = subprocess.check_output(["go", "test", "./share", "-run", "^TestHeaderScrollProbe$", "-count=1", "-v"], cwd=ROOT, text=True)
finally:
    probe.unlink()
pages = json.loads(output[output.index("{"):output.rindex("}") + 1])

with sync_playwright() as p:
    count = 0
    for engine in ("webkit", "chromium"):
        browser = getattr(p, engine).launch()
        for width, height in ((390, 844), (1440, 900)):
            for artifact in ("long", "clipped"):
                page = browser.new_page(viewport={"width": width, "height": height})
                body = pages[artifact]
                page.route("http://ferry.test/**", lambda route: route.fulfill(content_type="text/html", body=pages["preview"] if "/s/" in route.request.url else body))
                page.goto("http://ferry.test/s/test")
                frame = page.frame_locator("iframe")
                frame.locator("#first").wait_for()
                header = page.locator(".box-header")
                header_height = header.bounding_box()["height"]
                first = page.frames[1].locator("#first")
                pin = page.frames[1].locator("#pin")
                if artifact == "long":
                    page.wait_for_function("document.querySelector('.artifact-shell').classList.contains('is-overlay')")
                    # At rest the header must not cover the artifact's first line.
                    assert first.bounding_box()["y"] >= header_height - 1, (engine, width, "header covers content")
                    # Pinned UI using --ferry-inset-visible sits directly under the header.
                    assert abs(pin.bounding_box()["y"] - header_height) <= 1, (engine, width, "pinned UI not under header", pin.bounding_box())
                    assert abs(page.frames[1].evaluate("parseFloat(getComputedStyle(document.documentElement).getPropertyValue('--ferry-inset-top'))") - header_height) <= 1, (engine, width, "inset-top")
                    page.frames[1].evaluate("window.scrollTo(0, 30)")
                    page.wait_for_function("getComputedStyle(document.querySelector('.box-header')).transform.includes('-30')")
                    page.wait_for_timeout(100)
                    # Pinned UI follows the header as it slides away.
                    assert abs(pin.bounding_box()["y"] - (header_height - 30)) <= 1, (engine, width, "pinned UI does not follow header", pin.bounding_box())
                    page.frames[1].evaluate("window.scrollTo(0, 2000)")
                    page.wait_for_function(f"document.querySelector('.box-header').getBoundingClientRect().bottom <= 1")
                    page.wait_for_timeout(100)
                    assert abs(pin.bounding_box()["y"]) <= 1, (engine, width, "pinned UI not at top once header is gone", pin.bounding_box())
                    page.frames[1].evaluate("window.scrollTo(0, 0)")
                    page.wait_for_function("document.querySelector('.box-header').getBoundingClientRect().top >= -1")
                    page.wait_for_timeout(100)
                    assert abs(pin.bounding_box()["y"] - header_height) <= 1, (engine, width, "pinned UI not restored under header")
                    assert abs(page.locator("iframe").bounding_box()["height"] - height) <= 1, (engine, width, "frame not full height")
                else:
                    page.wait_for_timeout(300)
                    assert not page.evaluate("document.querySelector('.artifact-shell').classList.contains('is-overlay')"), (engine, width, "clipped artifact overlaid")
                    assert first.bounding_box()["y"] >= header_height - 1, (engine, width, "clipped content covered")
                    # The bridge bails, so the variables stay unset and pinned UI uses its fallback.
                    assert page.frames[1].evaluate("getComputedStyle(document.documentElement).getPropertyValue('--ferry-inset-visible')") == "", (engine, width, "clipped artifact got inset var")
                    assert abs(pin.bounding_box()["y"] - header_height) <= 1, (engine, width, "clipped pinned UI not at frame top")
                assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), "outer overflow"
                page.close()
                count += 1
        browser.close()
    print(f"PASS: {count} browser/size/artifact cases")
