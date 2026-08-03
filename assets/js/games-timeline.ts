import { Timeline } from "vis-timeline/standalone";
import type { DataItem, DataGroup } from "vis-timeline/standalone";

interface GameItem extends DataItem {
  permalink?: string;
}

function readJSON<T>(id: string): T | null {
  const el = document.getElementById(id);
  if (!el || !el.textContent) return null;
  try {
    return JSON.parse(el.textContent) as T;
  } catch {
    return null;
  }
}

function toTime(value: string | number | Date): number {
  return value instanceof Date ? value.getTime() : new Date(value).getTime();
}

const daySpan = 1000 * 60 * 60 * 24;

// Buffer around a window for counting a game as "near" it, as a fraction of the window's span.
const NEARBY_PADDING_RATIO = 0.5;

// Cap on the ratio-based padding so zooming out to years of history doesn't pull in far-off groups.
const MAX_PADDING_DAYS = 60;

// Initial view width; without a default it'd fit the entire multi-year history at once.
const DEFAULT_WINDOW_DAYS = 180;

function visibleGroupIds(
  items: GameItem[],
  range: { start: number; end: number },
): Set<unknown> {
  const span = range.end - range.start;
  const padding = Math.min(span * NEARBY_PADDING_RATIO, daySpan * MAX_PADDING_DAYS);
  const paddedStart = range.start - padding;
  const paddedEnd = range.end + padding;

  const ids = new Set<unknown>();
  for (const item of items) {
    const itemStart = toTime(item.start);
    const itemEnd = toTime(item.end as string);
    if (itemStart <= paddedEnd && itemEnd >= paddedStart) {
      ids.add(item.group);
    }
  }
  return ids;
}

function main(): void {
  const root = document.getElementById("games-timeline-root");
  const groups = readJSON<DataGroup[]>("games-timeline-groups");
  const items = readJSON<GameItem[]>("games-timeline-items");
  if (!root || !groups || !items || items.length === 0) return;

  const today = new Date().toISOString().slice(0, 10);
  for (const item of items) {
    if (!item.end) item.end = today;
  }

  const domainStart = Math.min(...items.map((item) => toTime(item.start)));
  const domainEnd = Math.max(...items.map((item) => toTime(item.end as string)));
  const domainSpan = Math.max(domainEnd - domainStart, daySpan * 30);

  root.replaceChildren();

  // Set via options.start/end, not a post-construction setWindow() — vis-timeline auto-fits
  // to the full item range on its initial draw and would silently override that call.
  const initialEnd = domainEnd;
  const initialStart = Math.max(domainStart, initialEnd - daySpan * DEFAULT_WINDOW_DAYS);

  // Clamp panning to the data range, with a two-week buffer so "today" isn't flush against the edge.
  const minDate = new Date(domainStart - daySpan * 14);
  const maxDate = new Date(Date.now() + daySpan * 14);

  const timeline = new Timeline(root, items, groups, {
    editable: false,
    selectable: true,
    stack: false,
    zoomable: false,
    zoomMin: daySpan * 30,
    zoomMax: Math.round(domainSpan * 1.2),
    start: initialStart,
    end: initialEnd,
    min: minDate,
    max: maxDate,
  });

  // zoomable: false disables vis-timeline's wheel/pinch zoom (the +/- buttons below call
  // zoomIn/zoomOut directly instead). But Chromium derives the legacy wheelDelta from a pure
  // horizontal scroll, which still hits vis-timeline's disabled zoom path oddly — so intercept
  // horizontal-dominant wheel events here and pan manually; vertical scrolling passes through untouched.
  const PAN_WHEEL_DIVISOR = 2400; // matches vis-timeline's own horizontalScroll formula (delta/120 * span/20)

  root.addEventListener(
    "wheel",
    (event: WheelEvent) => {
      if (Math.abs(event.deltaX) <= Math.abs(event.deltaY)) return;
      event.preventDefault();
      event.stopPropagation();
      const { start, end } = timeline.getWindow();
      const span = end.getTime() - start.getTime();
      const diff = (event.deltaX * span) / PAN_WHEEL_DIVISOR;
      timeline.setWindow(
        new Date(start.getTime() + diff),
        new Date(end.getTime() + diff),
        { animation: false },
      );
    },
    { capture: true, passive: false },
  );

  function applyVisibleGroups(range: { start: Date; end: Date }): void {
    const ids = visibleGroupIds(items, {
      start: range.start.getTime(),
      end: range.end.getTime(),
    });
    timeline.setGroups(
      groups.map((group) => ({ ...group, visible: ids.has(group.id) })),
    );
  }

  applyVisibleGroups({ start: new Date(initialStart), end: new Date(initialEnd) });

  timeline.on("rangechanged", () => {
    applyVisibleGroups(timeline.getWindow());
  });

  const todayBtn = document.getElementById("games-timeline-today");
  if (todayBtn) {
    todayBtn.hidden = false;
    todayBtn.addEventListener("click", () => {
      timeline.moveTo(new Date());
    });
  }

  const ZOOM_STEP = 0.2;

  const zoomInBtn = document.getElementById("games-timeline-zoom-in");
  if (zoomInBtn) {
    zoomInBtn.hidden = false;
    zoomInBtn.addEventListener("click", () => {
      timeline.zoomIn(ZOOM_STEP);
    });
  }

  const zoomOutBtn = document.getElementById("games-timeline-zoom-out");
  if (zoomOutBtn) {
    zoomOutBtn.hidden = false;
    zoomOutBtn.addEventListener("click", () => {
      timeline.zoomOut(ZOOM_STEP);
    });
  }

  timeline.on("select", (props: { items: Array<string | number> }) => {
    const id = props.items[0];
    if (id === undefined) return;
    const item = items.find((candidate) => candidate.id === id);
    if (item?.permalink) {
      window.location.href = item.permalink;
    }
  });
}

main();
