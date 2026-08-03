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

// How far outside a window a game still counts as "near" it, as a fraction of
// that window's own span — so the buffer scales with zoom level instead of
// being a fixed number of days.
const NEARBY_PADDING_RATIO = 0.5;

// Default width of the initial view. Without this, the timeline would default
// to fitting the *entire* history, which becomes a useless, tiny-barred mess
// after years of logged games — so default to a recent slice instead.
const DEFAULT_WINDOW_DAYS = 180;

function visibleGroupIds(
  items: GameItem[],
  range: { start: number; end: number },
): Set<unknown> {
  const span = range.end - range.start;
  const padding = span * NEARBY_PADDING_RATIO;
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

  const daySpan = 1000 * 60 * 60 * 24;
  const domainStart = Math.min(...items.map((item) => toTime(item.start)));
  const domainEnd = Math.max(...items.map((item) => toTime(item.end as string)));
  const domainSpan = Math.max(domainEnd - domainStart, daySpan * 30);

  root.replaceChildren();

  // Passed as options.start/end (rather than setWindow() after construction)
  // because vis-timeline auto-fits to the full item range on its own initial
  // draw when no start/end is given, which would silently override a
  // post-construction setWindow() call.
  const initialEnd = domainEnd;
  const initialStart = Math.max(domainStart, initialEnd - daySpan * DEFAULT_WINDOW_DAYS);

  const timeline = new Timeline(root, items, groups, {
    editable: false,
    selectable: true,
    stack: false,
    zoomMin: daySpan * 30,
    zoomMax: Math.round(domainSpan * 1.2),
    start: initialStart,
    end: initialEnd,
  });

  // vis-timeline's zoom handler checks the legacy, non-standard
  // event.wheelDelta property before the modern deltaY — and Chromium
  // derives wheelDelta from the horizontal delta when there's no vertical
  // motion. So a pure horizontal mouse-wheel notch still reads as a zoom
  // tick. Intercept horizontal-dominant wheel gestures in the capture
  // phase (before they reach vis-timeline's own listeners) and pan the
  // window manually instead. Vertical-dominant scrolling is left alone
  // and still zooms as before.
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
