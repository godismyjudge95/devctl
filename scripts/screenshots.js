#!/usr/bin/env node
/**
 * Rebuild dashboard screenshots for the README.
 *
 * Usage:
 *   cd scripts && npm install && node screenshots.js
 *
 * Output: ../docs/screenshot-{services,sites,dumps,settings}.png
 *
 * Requirements: devctl must be running at http://127.0.0.1:4000
 */

import puppeteer from "puppeteer";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const BASE_URL = "http://127.0.0.1:4000";
const OUT_DIR = path.resolve(__dirname, "../docs");

const PAGES = [
  { route: "/services", filename: "screenshot-services.png" },
  { route: "/sites",    filename: "screenshot-sites.png"    },
  { route: "/dumps",    filename: "screenshot-dumps.png"    },
  { route: "/settings", filename: "screenshot-settings.png" },
];

const VIEWPORT = { width: 1400, height: 860 };

async function main() {
  const browser = await puppeteer.launch({
    headless: true,
    args: ["--no-sandbox", "--disable-setuid-sandbox"],
  });

  try {
    const page = await browser.newPage();
    await page.setViewport(VIEWPORT);

    for (const { route, filename } of PAGES) {
      const url = `${BASE_URL}${route}`;
      const outPath = path.join(OUT_DIR, filename);

      console.log(`  → ${url}`);
      await page.goto(url, { waitUntil: "networkidle2", timeout: 15_000 });

      // Allow any transition animations to settle
      await new Promise((r) => setTimeout(r, 400));

      await page.screenshot({ path: outPath, type: "png" });
      console.log(`    saved ${outPath}`);
    }
  } finally {
    await browser.close();
  }

  console.log("\nDone.");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
