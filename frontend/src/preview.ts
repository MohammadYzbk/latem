// The PDF preview pane.
//
// Each page is a canvas inside a positioned wrapper, which is what makes the two
// SyncTeX directions possible: a click can be resolved to a page and a point in
// PDF coordinates, and a highlight can be positioned over the page without
// redrawing it.

// The *legacy* build, not the modern one. PDF.js 6.x calls
// `Map.prototype.getOrInsertComputed`, which the macOS WKWebView (15.7) does not
// implement — the modern build dies with "getOrInsertComputed is not a function"
// on the first render. The legacy bundle carries the core-js polyfill for it.
// Since we ship against whatever webview the OS provides, legacy is the correct
// default for a Wails app; revisit only if the bundle size starts to hurt.
import * as pdfjs from 'pdfjs-dist/legacy/build/pdf.mjs';
import workerUrl from 'pdfjs-dist/legacy/build/pdf.worker.mjs?url';
import type { synctex } from '../wailsjs/go/models';

// PDF.js needs its worker as a separate module. Letting Vite hash the URL keeps
// it working in both `wails dev` and the bundled asset server.
pdfjs.GlobalWorkerOptions.workerSrc = workerUrl;

// CSS pixels per PDF point, recomputed per render so a page fits the pane.
//
// A fixed scale was wrong in both directions: too large and the page overflows a
// narrow preview pane and is clipped with no way to scroll to it; and the obvious
// patch — max-width on the canvas — makes CSS shrink the bitmap so the rendered
// page no longer matches the scale the SyncTeX maths uses, putting every
// highlight and every click in the wrong place. One scale, used everywhere.
const MIN_SCALE = 0.35;
const MAX_SCALE = 2;
const PAGE_PADDING = 32;

export interface RenderInfo {
  pages: number;
  widthPt: number;
  heightPt: number;
}

export interface PreviewOptions {
  /** A click on a page, in PDF points from that page's top-left corner. */
  onPointClicked(page: number, x: number, y: number): void;
}

export interface PreviewHandle {
  /** Fetches and renders a PDF, keeping the reader's place. */
  load(url: string): Promise<RenderInfo>;
  /** Draws the forward-search highlight, replacing any previous one. */
  highlight(rects: synctex.Rect[]): void;
  clearHighlight(): void;
  /** Brings a rect into view. `center` forces a scroll even if already visible. */
  reveal(rect: synctex.Rect, center: boolean): void;
  /** Shows a message in place of a document. */
  showMessage(text: string): void;
}

export function mountPreview(container: HTMLElement, opts: PreviewOptions): PreviewHandle {
  // The scale of the current render. Highlights and clicks must use exactly this
  // value, not the one a future render would pick.
  let scale = 1;

  container.addEventListener('click', (event) => {
    const target = event.target as HTMLElement | null;
    const page = target?.closest<HTMLElement>('.page');
    if (!page) return;

    const pageNumber = Number(page.dataset.page);
    if (!pageNumber) return;

    // The wrapper's box is the page's box, so the offset within it converts to
    // PDF points by a single division.
    const box = page.getBoundingClientRect();
    opts.onPointClicked(pageNumber, (event.clientX - box.left) / scale, (event.clientY - box.top) / scale);
  });

  const pageElement = (page: number) =>
    container.querySelector<HTMLElement>(`.page[data-page="${page}"]`);

  function clearHighlight() {
    container.querySelectorAll('.sync-mark').forEach((mark) => mark.remove());
  }

  function highlight(rects: synctex.Rect[]) {
    clearHighlight();
    for (const rect of rects) {
      const page = pageElement(rect.page);
      if (!page) continue;
      const mark = document.createElement('div');
      mark.className = 'sync-mark';
      mark.style.left = `${rect.x * scale}px`;
      mark.style.top = `${rect.y * scale}px`;
      mark.style.width = `${Math.max(rect.width * scale, 2)}px`;
      mark.style.height = `${Math.max(rect.height * scale, 2)}px`;
      page.append(mark);
    }
  }

  function reveal(rect: synctex.Rect, center: boolean) {
    const page = pageElement(rect.page);
    if (!page) return;

    const top = page.offsetTop + rect.y * scale;
    const bottom = top + rect.height * scale;
    const viewTop = container.scrollTop;
    const viewBottom = viewTop + container.clientHeight;

    // Only move the page when the target is actually out of sight. Scrolling on
    // every cursor movement makes the preview twitch while you type.
    const visible = top >= viewTop + 8 && bottom <= viewBottom - 8;
    if (visible && !center) return;

    container.scrollTo({
      top: Math.max(0, top - container.clientHeight / 2 + (rect.height * scale) / 2),
      behavior: 'smooth',
    });
  }

  function showMessage(text: string) {
    const p = document.createElement('p');
    p.className = 'empty';
    p.textContent = text;
    container.replaceChildren(p);
  }

  async function load(url: string): Promise<RenderInfo> {
    const resp = await fetch(url);
    if (!resp.ok) throw new Error(`fetching PDF: ${resp.status} ${resp.statusText}`);
    const data = new Uint8Array(await resp.arrayBuffer());
    const doc = await pdfjs.getDocument({ data }).promise;

    // Fit the page to the pane before rendering anything, so every page and every
    // coordinate derived from it agree.
    const firstPage = await doc.getPage(1);
    const pageWidthPt = firstPage.getViewport({ scale: 1 }).width;
    const available = Math.max(container.clientWidth - PAGE_PADDING, 120);
    scale = Math.min(MAX_SCALE, Math.max(MIN_SCALE, available / pageWidthPt));

    // Where the reader was, as a fraction of the scrollable range. A fraction
    // rather than a pixel offset because the document can gain or lose pages
    // between compiles; when the height is unchanged the arithmetic reproduces
    // the exact same offset anyway.
    const scrollable = container.scrollHeight - container.clientHeight;
    const position = scrollable > 0 ? container.scrollTop / scrollable : 0;

    // Render into a detached fragment and swap it in at the end. Nothing on
    // screen changes until every page is drawn, so the pane never flashes empty
    // or half-drawn — and a failure part-way through leaves the previous render
    // untouched.
    const staged = document.createDocumentFragment();
    const dpr = window.devicePixelRatio || 1;
    let widthPt = 0;
    let heightPt = 0;

    for (let n = 1; n <= doc.numPages; n++) {
      const page = n === 1 ? firstPage : await doc.getPage(n);
      const viewport = page.getViewport({ scale });
      if (n === 1) {
        const unscaled = page.getViewport({ scale: 1 });
        widthPt = unscaled.width;
        heightPt = unscaled.height;
      }

      const wrap = document.createElement('div');
      wrap.className = 'page';
      wrap.dataset.page = String(n);
      wrap.style.width = `${Math.floor(viewport.width)}px`;
      wrap.style.height = `${Math.floor(viewport.height)}px`;

      const canvas = document.createElement('canvas');
      canvas.className = 'pdf-page';
      // Back the canvas at device resolution but lay it out in CSS pixels, so
      // text stays crisp on a Retina display.
      canvas.width = Math.floor(viewport.width * dpr);
      canvas.height = Math.floor(viewport.height * dpr);
      canvas.style.width = `${Math.floor(viewport.width)}px`;
      canvas.style.height = `${Math.floor(viewport.height)}px`;
      wrap.append(canvas);
      staged.append(wrap);

      await page.render({
        canvas,
        viewport,
        transform: dpr === 1 ? undefined : [dpr, 0, 0, dpr, 0, 0],
      }).promise;
    }

    container.replaceChildren(staged);

    // Restore after the swap, in the same frame, so the reader never sees the
    // pane jump to the top and back.
    const nowScrollable = container.scrollHeight - container.clientHeight;
    if (nowScrollable > 0) container.scrollTop = position * nowScrollable;

    return { pages: doc.numPages, widthPt, heightPt };
  }

  return { load, highlight, clearHighlight, reveal, showMessage };
}
