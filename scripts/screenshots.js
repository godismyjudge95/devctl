#!/usr/bin/env node
/**
 * Take dashboard screenshots for every page of the README.
 *
 * Usage (standalone — devctl must be running at BASE_URL):
 *   cd scripts && node screenshots.js
 *
 * Usage (via demo.sh — targets the demo container):
 *   BASE_URL=http://127.0.0.1:4001 node scripts/screenshots.js
 *
 * Output — all files land in docs/:
 *   Desktop (1400×860):
 *     screenshot-services.png
 *     screenshot-sites.png
 *     screenshot-dumps.png
 *     screenshot-mail.png       (first message auto-selected for detail panel)
 *     screenshot-spx.png        (first profile auto-selected for detail panel)
 *     screenshot-logs.png       (first log file auto-selected)
 *     screenshot-settings.png
 *     screenshot-maxio.png
 *     screenshot-whodb.png
 *
 *   Mobile (390×844):
 *     screenshot-mobile-services.png
 *     screenshot-mobile-sites.png
 *     screenshot-mobile-dumps.png
 *     screenshot-mobile-mail.png
 *     screenshot-mobile-spx.png
 *     screenshot-mobile-logs.png
 *     screenshot-mobile-settings.png
 *     screenshot-mobile-maxio.png
 *     screenshot-mobile-whodb.png
 */

import puppeteer from "puppeteer";
import path from "path";
import { fileURLToPath } from "url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const BASE_URL  = process.env.BASE_URL || "http://127.0.0.1:4000";
const OUT_DIR   = path.resolve(__dirname, "../docs");

const DESKTOP = { width: 1400, height: 860 };
const MOBILE  = { width: 390,  height: 844, isMobile: true, hasTouch: true };

// ─── Helpers ──────────────────────────────────────────────────────────────────

function sleep(ms) {
  return new Promise(r => setTimeout(r, ms));
}

/** Navigate and wait for network to settle. */
async function nav(page, route) {
  await page.goto(`${BASE_URL}${route}`, { waitUntil: "networkidle2", timeout: 20_000 });
  await sleep(500);
}

/** Click the first element matching selector, if present. */
async function clickFirst(page, selector, afterMs = 800) {
  try {
    const el = await page.$(selector);
    if (el) {
      await el.click();
      await sleep(afterMs);
    }
  } catch (_) { /* element gone or not present — that's fine */ }
}

/** Save a screenshot to docs/. */
async function snap(page, filename) {
  const outPath = path.join(OUT_DIR, filename);
  await page.screenshot({ path: outPath, type: "png" });
  console.log(`  saved ${outPath}`);
}

// ─── Page definitions ─────────────────────────────────────────────────────────
//
// Each entry has:
//   route      — Vue router path
//   desktop    — desktop output filename
//   mobile     — mobile output filename
//   before(page) — optional async setup run *after* navigate + settle,
//                  *before* the screenshot. Use for click interactions.
//   extraWait  — additional ms to wait after navigate (for iframe-heavy pages)

const PAGES = [
  {
    route:   "/services",
    desktop: "screenshot-services.png",
    mobile:  "screenshot-mobile-services.png",
  },
  {
    route:   "/sites",
    desktop: "screenshot-sites.png",
    mobile:  "screenshot-mobile-sites.png",
  },
  {
    route:   "/dumps",
    desktop: "screenshot-dumps.png",
    mobile:  "screenshot-mobile-dumps.png",
  },
  {
    // Mail: click the first message row so the detail panel is visible.
    // Message rows have class "cursor-pointer" in MailView.vue.
    route:   "/mail",
    desktop: "screenshot-mail.png",
    mobile:  "screenshot-mobile-mail.png",
    async before(page) {
      // Wait for at least one message to appear in the list.
      await page.waitForFunction(
        () => document.querySelectorAll(".cursor-pointer").length > 0,
        { timeout: 5000 }
      ).catch(() => {});
      await clickFirst(page, ".cursor-pointer", 800);
    },
  },
  {
    // SPX: click the first profile row so the detail panel is visible.
    // Profile rows also use class "cursor-pointer" in SpxView.vue.
    route:   "/spx",
    desktop: "screenshot-spx.png",
    mobile:  "screenshot-mobile-spx.png",
    async before(page) {
      await page.waitForFunction(
        () => document.querySelectorAll(".cursor-pointer").length > 0,
        { timeout: 5000 }
      ).catch(() => {});
      await clickFirst(page, ".cursor-pointer", 800);
    },
  },
  {
    // Logs: click the first log file button so the viewer pane is populated.
    // Log file items are <button> elements inside <aside>.
    route:   "/logs",
    desktop: "screenshot-logs.png",
    mobile:  "screenshot-mobile-logs.png",
    async before(page) {
      await page.waitForSelector("aside button", { timeout: 5000 }).catch(() => {});
      await clickFirst(page, "aside button", 1000);
    },
  },
  {
    route:   "/settings",
    desktop: "screenshot-settings.png",
    mobile:  "screenshot-mobile-settings.png",
  },
  {
    // MaxIO: custom file-browser UI — give it a moment to load bucket list.
    route:      "/maxio",
    desktop:    "screenshot-maxio.png",
    mobile:     "screenshot-mobile-maxio.png",
    extraWait:  1000,
  },
  {
    // WhoDB: embeds an iframe at http://127.0.0.1:8161.
    // The demo.sh proxy device forwards host:8161 → container:8161 so the
    // iframe loads correctly when running against the demo container.
    route:      "/whodb",
    desktop:    "screenshot-whodb.png",
    mobile:     "screenshot-mobile-whodb.png",
    extraWait:  4000,   // allow the iframe time to fully render
  },
];

// ─── Main ─────────────────────────────────────────────────────────────────────

async function main() {
  const browser = await puppeteer.launch({
    headless: true,
    args: ["--no-sandbox", "--disable-setuid-sandbox"],
  });

  try {
    // ── Desktop ──────────────────────────────────────────────────────────────
    console.log("\nDesktop screenshots (1400×860):");
    const desktopPage = await browser.newPage();
    await desktopPage.setViewport(DESKTOP);

    for (const def of PAGES) {
      console.log(`  → ${BASE_URL}${def.route}`);
      await nav(desktopPage, def.route);
      if (def.extraWait) await sleep(def.extraWait);
      if (def.before) await def.before(desktopPage);
      await snap(desktopPage, def.desktop);
    }
    await desktopPage.close();

    // ── Mobile ───────────────────────────────────────────────────────────────
    console.log("\nMobile screenshots (390×844):");
    const mobilePage = await browser.newPage();
    await mobilePage.setViewport(MOBILE);

    for (const def of PAGES) {
      console.log(`  → ${BASE_URL}${def.route}`);
      await nav(mobilePage, def.route);
      if (def.extraWait) await sleep(def.extraWait);
      if (def.before) await def.before(mobilePage);
      await snap(mobilePage, def.mobile);
    }
    await mobilePage.close();

  } finally {
    await browser.close();
  }

  console.log("\nDone — screenshots saved to docs/");
}

main().catch(err => {
  console.error(err);
  process.exit(1);
});
