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

// CSS pixels per PDF point.
//
// A fixed scale was wrong in both directions: too large and the page overflows a
// narrow preview pane and is clipped with no way to scroll to it; and the obvious
// patch — max-width on the canvas — makes CSS shrink the bitmap so the rendered
// page no longer matches the scale the SyncTeX maths uses, putting every
// highlight and every click in the wrong place. One scale, used everywhere.
const MIN_SCALE = 0.25;
// Generous, because zooming in is how you check a figure or a subscript. The
// canvas is backed at this scale times the device ratio, so the ceiling is what
// stops a long document from exhausting memory.
const MAX_SCALE = 6;
const PAGE_PADDING = 32;

/** Multiplier per zoom step: roughly the 1.2 that PDF viewers settled on. */
const ZOOM_STEP = 1.2;

// How long a pinch has to pause before the pages are redrawn at the new scale.
// Short enough to feel like it is keeping up, long enough that a single gesture
// does not rasterise the document a dozen times.
const ZOOM_SETTLE_MS = 90;

/** 'fit' tracks the pane width; a number is a scale the reader chose. */
export type Zoom = 'fit' | number;

export interface RenderInfo {
  pages: number;
  widthPt: number;
  heightPt: number;
}

export interface PreviewOptions {
  /** A click on a page, in PDF points from that page's top-left corner. */
  onPointClicked(page: number, x: number, y: number): void;
  /** Called whenever the effective scale changes, for the zoom readout. */
  onZoomChanged?(zoom: Zoom, scale: number): void;
}

export interface PreviewHandle {
  /** Fetches and renders a PDF, keeping the reader's place and zoom. */
  load(url: string): Promise<RenderInfo>;
  /** Draws the forward-search highlight, replacing any previous one. */
  highlight(rects: synctex.Rect[]): void;
  clearHighlight(): void;
  /** Brings a rect into view. `center` forces a scroll even if already visible. */
  reveal(rect: synctex.Rect, center: boolean): void;
  /** Shows a message in place of a document. */
  showMessage(text: string): void;
  zoomIn(): void;
  zoomOut(): void;
  /** Multiplies the current scale — for a pinch, which is continuous. */
  zoomBy(factor: number): void;
  /** Returns to tracking the pane width. */
  fitWidth(): void;
  /** Renders at exactly 100%: one CSS pixel per PDF point. */
  actualSize(): void;
  zoom(): Zoom;
  scale(): number;
}

export function mountPreview(container: HTMLElement, opts: PreviewOptions): PreviewHandle {
  // The scale of the current render. Highlights and clicks must use exactly this
  // value, not the one a future render would pick.
  let scale = 1;
  let zoom: Zoom = 'fit';

  // The open document is kept so zooming can redraw without refetching, and the
  // first page's width so a fit can be recomputed without touching the document
  // at all.
  let doc: pdfjs.PDFDocumentProxy | null = null;
  // The loading task, not the document, owns the worker transport — tearing it
  // down is what actually frees the previous PDF.
  let task: ReturnType<typeof pdfjs.getDocument> | null = null;
  let pageWidthPt = 0;

  // Renders are async and can overlap — a zoom during a compile, or a held-down
  // zoom key. Only the newest may touch the DOM.
  let generation = 0;

  // The last highlight, reapplied after a redraw. Without this, zooming silently
  // drops the marker and the preview stops agreeing with the cursor.
  let lastRects: synctex.Rect[] = [];

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

  // Fit follows the pane, so widening the preview has to redraw. Debounced
  // because a drag fires this continuously and each redraw rasterises every
  // page.
  let resizeTimer: number | undefined;
  new ResizeObserver(() => {
    if (zoom !== 'fit' || !doc) return;
    window.clearTimeout(resizeTimer);
    resizeTimer = window.setTimeout(() => {
      if (Math.abs(fitScale() - scale) > 0.005) void render();
    }, 120);
  }).observe(container);

  const pageElement = (page: number) =>
    container.querySelector<HTMLElement>(`.page[data-page="${page}"]`);

  function clamp(value: number): number {
    return Math.min(MAX_SCALE, Math.max(MIN_SCALE, value));
  }

  function fitScale(): number {
    if (pageWidthPt <= 0) return 1;
    const available = Math.max(container.clientWidth - PAGE_PADDING, 120);
    return clamp(available / pageWidthPt);
  }

  function resolveScale(): number {
    return zoom === 'fit' ? fitScale() : clamp(zoom);
  }

  function clearHighlight() {
    lastRects = [];
    container.querySelectorAll('.sync-mark').forEach((mark) => mark.remove());
  }

  function highlight(rects: synctex.Rect[]) {
    lastRects = rects;
    paintHighlight();
  }

  function paintHighlight() {
    container.querySelectorAll('.sync-mark').forEach((mark) => mark.remove());
    for (const rect of lastRects) {
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

  // --- zoom --------------------------------------------------------------------

  function setZoom(next: Zoom) {
    const before = resolveScale();
    zoom = typeof next === 'number' ? clamp(next) : next;
    if (!doc) {
      opts.onZoomChanged?.(zoom, resolveScale());
      return;
    }
    if (Math.abs(resolveScale() - before) < 0.001) {
      // Already there — at a limit, or fit happens to equal the chosen scale.
      opts.onZoomChanged?.(zoom, resolveScale());
      return;
    }
    void render({ anchor: 'centre' });
  }

  function zoomIn() {
    setZoom(resolveScale() * ZOOM_STEP);
  }

  function zoomOut() {
    setZoom(resolveScale() / ZOOM_STEP);
  }

  // A pinch delivers dozens of events a second, and each redraw rasterises
  // every page. So the target scale accumulates immediately — the readout
  // tracks the fingers — while the redraw waits for a pause in the gesture.
  let pending: number | null = null;
  let pendingTimer: number | undefined;

  function zoomBy(factor: number) {
    if (!Number.isFinite(factor) || factor <= 0) return;

    const next = clamp((pending ?? resolveScale()) * factor);
    if (next === pending) return;
    pending = next;
    opts.onZoomChanged?.(next, next);

    window.clearTimeout(pendingTimer);
    pendingTimer = window.setTimeout(() => {
      const target = pending;
      pending = null;
      if (target !== null) setZoom(target);
    }, ZOOM_SETTLE_MS);
  }

  // --- rendering ---------------------------------------------------------------

  /**
   * Draws every page at the current scale.
   *
   * `anchor` decides what to keep still. A compile keeps the reader's position
   * as a fraction of the document, because pages can appear or disappear
   * between runs. A zoom keeps whatever was in the middle of the pane in the
   * middle of the pane, which is what makes zooming feel like it is centred on
   * what you were reading rather than on the top of the file.
   */
  async function render(options: { anchor?: 'fraction' | 'centre' } = {}): Promise<RenderInfo | null> {
    if (!doc) return null;
    const mine = ++generation;
    const anchor = options.anchor ?? 'fraction';

    const scrollable = container.scrollHeight - container.clientHeight;
    const fraction = scrollable > 0 ? container.scrollTop / scrollable : 0;
    // Zoom anchoring works in scrolled pixels rather than a fraction of the
    // document, so the ratio between the two scales converts it directly.
    const centre = container.scrollTop + container.clientHeight / 2;
    const wasScrollable = scrollable > 1;
    const previousScale = scale;

    scale = resolveScale();

    // Render into a detached fragment and swap it in at the end. Nothing on
    // screen changes until every page is drawn, so the pane never flashes empty
    // or half-drawn — and a failure part-way through leaves the previous render
    // untouched.
    const staged = document.createDocumentFragment();
    const dpr = window.devicePixelRatio || 1;
    let widthPt = 0;
    let heightPt = 0;

    for (let n = 1; n <= doc.numPages; n++) {
      const page = await doc.getPage(n);
      if (mine !== generation) return null;

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
      if (mine !== generation) return null;
    }

    container.replaceChildren(staged);

    // Restore after the swap, in the same frame, so the reader never sees the
    // pane jump to the top and back.
    if (anchor === 'centre') {
      // A document that already fitted has all its content at the top, so the
      // pane's centre was blank space below it — anchoring there would zoom
      // the reader into an empty margin. Only a scrolled view has a centre
      // worth preserving.
      container.scrollTop = wasScrollable
        ? Math.max(0, (centre * scale) / previousScale - container.clientHeight / 2)
        : 0;
    } else {
      const nowScrollable = container.scrollHeight - container.clientHeight;
      if (nowScrollable > 0) container.scrollTop = fraction * nowScrollable;
    }

    paintHighlight();
    opts.onZoomChanged?.(zoom, scale);

    return { pages: doc.numPages, widthPt, heightPt };
  }

  async function load(url: string): Promise<RenderInfo> {
    const resp = await fetch(url);
    if (!resp.ok) throw new Error(`fetching PDF: ${resp.status} ${resp.statusText}`);
    const data = new Uint8Array(await resp.arrayBuffer());
    const nextTask = pdfjs.getDocument({ data });
    const next = await nextTask.promise;

    // Release the previous document only once the new one is in hand, so a
    // failed fetch leaves the current render usable.
    const previousTask = task;
    task = nextTask;
    doc = next;
    void previousTask?.destroy();

    // The fit depends on the page width, so it has to be known before the first
    // page is drawn — every coordinate derived from the scale depends on it.
    pageWidthPt = (await next.getPage(1)).getViewport({ scale: 1 }).width;

    // Deliberately not resetting the zoom: a recompile that snapped the reader
    // back to fit would undo their zoom several times a minute.
    const info = await render({ anchor: 'fraction' });
    if (!info) throw new Error('preview: render was superseded');
    return info;
  }

  return {
    load,
    highlight,
    clearHighlight,
    reveal,
    showMessage,
    zoomIn,
    zoomOut,
    zoomBy,
    fitWidth: () => setZoom('fit'),
    actualSize: () => setZoom(1),
    zoom: () => zoom,
    scale: () => scale,
  };
}
