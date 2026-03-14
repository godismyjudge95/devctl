#!/usr/bin/env node
/**
 * Rebuild dashboard screenshots for the README.
 *
 * Usage:
 *   cd scripts && npm install && node screenshots.js
 *
 * Output:
 *   ../docs/screenshot-{services,sites,dumps,settings}.png        (desktop 1400×860)
 *   ../docs/screenshot-mobile-{services,sites,mail}.png           (mobile 390×844)
 *
 * Requirements: devctl must be running at http://127.0.0.1:4000
 */

import puppeteer from "puppeteer";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const BASE_URL = "http://127.0.0.1:4000";
const OUT_DIR = path.resolve(__dirname, "../docs");

const DESKTOP_PAGES = [
  { route: "/services", filename: "screenshot-services.png" },
  { route: "/sites",    filename: "screenshot-sites.png"    },
  { route: "/dumps",    filename: "screenshot-dumps.png"    },
  { route: "/settings", filename: "screenshot-settings.png" },
];

const MOBILE_PAGES = [
  { route: "/services", filename: "screenshot-mobile-services.png" },
  { route: "/sites",    filename: "screenshot-mobile-sites.png"    },
  { route: "/mail",     filename: "screenshot-mobile-mail.png"     },
];

const DESKTOP_VIEWPORT = { width: 1400, height: 860 };
const MOBILE_VIEWPORT  = { width: 390,  height: 844, isMobile: true, hasTouch: true };

async function capturePages(page, pages, viewport) {
  await page.setViewport(viewport);
  for (const { route, filename } of pages) {
    const url = `${BASE_URL}${route}`;
    const outPath = path.join(OUT_DIR, filename);

    console.log(`  → ${url}`);
    await page.goto(url, { waitUntil: "networkidle2", timeout: 15_000 });

    // Allow any transition animations to settle
    await new Promise((r) => setTimeout(r, 400));

    await page.screenshot({ path: outPath, type: "png" });
    console.log(`    saved ${outPath}`);
  }
}

async function main() {
  const browser = await puppeteer.launch({
    headless: true,
    args: ["--no-sandbox", "--disable-setuid-sandbox"],
  });

  try {
    const page = await browser.newPage();

    console.log("\nDesktop screenshots:");
    await capturePages(page, DESKTOP_PAGES, DESKTOP_VIEWPORT);

    console.log("\nMobile screenshots:");
    await capturePages(page, MOBILE_PAGES, MOBILE_VIEWPORT);
  } finally {
    await browser.close();
  }

  console.log("\nDone.");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
