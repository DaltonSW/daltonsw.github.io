import { Timeline } from "vis-timeline/standalone";
import type { DataItem, DataGroup } from "vis-timeline/standalone";

interface GameItem extends DataItem {
  permalink?: string;
  status?: string;
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

// Gap beyond which two sessions of the same playthrough no longer bridge into one bar.
const CONNECTOR_GAP_DAYS = 180;

// These have no save file or arc to bridge, so sessions never chain into one bar.
const NO_CONNECTOR_CLASSES = ["pt--ongoing", "pt--multiplayer", "pt--software"];

// Derives dim bridging bars between sessions client-side, split per group+subgroup at gaps > CONNECTOR_GAP_DAYS.
function buildConnectors(items: GameItem[]): GameItem[] {
  const bySubgroup = new Map<string, GameItem[]>();
  for (const item of items) {
    if (!item.className?.includes("pt-session")) continue;
    if (NO_CONNECTOR_CLASSES.some((cls) => item.className?.includes(cls))) continue;
    const key = `${item.group}::${item.subgroup}`;
    const list = bySubgroup.get(key);
    if (list) list.push(item);
    else bySubgroup.set(key, [item]);
  }

  const connectors: GameItem[] = [];
  for (const [key, sessions] of bySubgroup) {
    sessions.sort((a, b) => toTime(a.start) - toTime(b.start));

    let clusterStart = sessions[0];
    let clusterEnd = sessions[0];
    const flush = (last: GameItem) => {
      if (last === clusterStart) return; // lone session — nothing to bridge
      connectors.push({
        id: `${key}--connector--${clusterStart.id}`,
        group: clusterStart.group,
        subgroup: clusterStart.subgroup,
        type: "range",
        className: (clusterStart.className ?? "").replace("pt-session", "pt-connector"),
        content: "",
        start: clusterStart.start,
        end: last.end,
      } as GameItem);
    };

    for (let i = 1; i < sessions.length; i++) {
      const gapDays = (toTime(sessions[i].start) - toTime(clusterEnd.end as string)) / daySpan;
      if (gapDays > CONNECTOR_GAP_DAYS) {
        flush(clusterEnd);
        clusterStart = sessions[i];
      }
      clusterEnd = sessions[i];
    }
    flush(clusterEnd);
  }
  return connectors;
}

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

// Breathing room below the chart; generous since font-metric rounding can otherwise
// leave a stray page scrollbar with nothing to scroll.
const HEIGHT_GAP = 32;

// Floor so the chart doesn't collapse to nothing on very short viewports.
const MIN_ROOT_HEIGHT = 240;

// Bounds the chart to the remaining (measured, not hardcoded) viewport space below
// it, so vis-timeline scrolls internally instead of growing the page.
function applyRootHeight(root: HTMLElement): void {
  const top = root.getBoundingClientRect().top;

  // .games-timeline's own margin/border between root and whatever's below it.
  const box = root.closest<HTMLElement>(".games-timeline");
  const boxStyle = box ? getComputedStyle(box) : null;
  const belowBox = boxStyle
    ? parseFloat(boxStyle.marginBottom) + parseFloat(boxStyle.borderBottomWidth)
    : 0;

  const main = document.querySelector<HTMLElement>(".site-main");
  const belowPadding = main ? parseFloat(getComputedStyle(main).paddingBottom) : 0;
  const footer = document.querySelector<HTMLElement>(".site-footer");
  const footerHeight = footer ? footer.getBoundingClientRect().height : 0;
  const height = Math.max(
    window.innerHeight - top - belowBox - belowPadding - footerHeight - HEIGHT_GAP,
    MIN_ROOT_HEIGHT,
  );
  root.style.height = `${height}px`;
}

function main(): void {
  const root = document.getElementById("games-timeline-root");
  const groups = readJSON<DataGroup[]>("games-timeline-groups");
  const items = readJSON<GameItem[]>("games-timeline-items");
  if (!root || !groups || !items || items.length === 0) return;

  // Deferred a frame so tabs.ts has already revealed the tab strip (it runs later
  // in document order); otherwise root's measured position lands too high.
  requestAnimationFrame(() => renderTimeline(root, groups, items));
}

function renderTimeline(root: HTMLElement, groups: DataGroup[], items: GameItem[]): void {
  const today = new Date().toISOString().slice(0, 10);
  for (const item of items) {
    if (!item.end) item.end = today;
  }
  items.push(...buildConnectors(items));

  const domainStart = Math.min(...items.map((item) => toTime(item.start)));
  const domainEnd = Math.max(...items.map((item) => toTime(item.end as string)));
  const domainSpan = Math.max(domainEnd - domainStart, daySpan * 30);

  root.replaceChildren();
  applyRootHeight(root);

  // Must be set via options.start/end — a post-construction setWindow() gets overridden by vis-timeline's initial auto-fit.
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
    // Fills root's fixed height; group rows then scroll internally, axis pinned.
    height: "100%",
    // Defaults to false — without it, overflow content just clips with no scroll.
    verticalScroll: true,
    locale: "en",
    orientation: { axis: "top" },
    tooltip: {
      followMouse: true,
      overflowMethod: "cap",
    },
    template: (item: GameItem) =>
      item.className?.includes("pt-connector") ? "" : (item.status ?? ""),
  });

  // vis-timeline's own scrollbar is a thin bar flush against the row labels — fade
  // the top/bottom edges in when there's more to scroll to. .vis-panel.vis-left is
  // the panel vis-timeline gives a real scrollbar (.vis-center just mirrors it).
  const scrollPanel = root.querySelector<HTMLElement>(".vis-panel.vis-left");
  const axisPanel = root.querySelector<HTMLElement>(".vis-panel.vis-top");

  // Chromium routes horizontal trackpad scroll into vis-timeline's zoom path, so pan
  // manually here; vertical scroll is also driven manually so it clamps instead of
  // rubber-banding past its bounds (native overflow-y: scroll bounces on trackpads).
  const PAN_WHEEL_DIVISOR = 2400; // vis-timeline's own horizontalScroll formula
  let userScrolled = false;

  root.addEventListener(
    "wheel",
    (event: WheelEvent) => {
      if (Math.abs(event.deltaX) > Math.abs(event.deltaY)) {
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
        return;
      }
      if (!scrollPanel) return;
      event.preventDefault();
      userScrolled = true;
      const max = scrollPanel.scrollHeight - scrollPanel.clientHeight;
      scrollPanel.scrollTop = Math.min(Math.max(scrollPanel.scrollTop + event.deltaY, 0), max);
    },
    { capture: true, passive: false },
  );

  // Panning changes which groups are visible, which changes the scrollable height —
  // refreshFades gets called after that too, not just on scroll.
  let refreshFades = (): void => {};
  if (scrollPanel) {
    const fadeTop = document.createElement("div");
    fadeTop.className = "games-timeline__fade games-timeline__fade--top";
    const fadeBottom = document.createElement("div");
    fadeBottom.className = "games-timeline__fade games-timeline__fade--bottom";
    root.append(fadeTop, fadeBottom);
    if (axisPanel) {
      root.style.setProperty("--games-timeline-axis-height", `${axisPanel.getBoundingClientRect().height}px`);
    }

    const FADE_SLACK = 2;
    const updateFades = (): void => {
      const canScrollUp = scrollPanel.scrollTop > FADE_SLACK;
      const canScrollDown =
        scrollPanel.scrollTop + scrollPanel.clientHeight < scrollPanel.scrollHeight - FADE_SLACK;
      root.classList.toggle("games-timeline__root--can-scroll-up", canScrollUp);
      root.classList.toggle("games-timeline__root--can-scroll-down", canScrollDown);
    };
    scrollPanel.addEventListener("scroll", updateFades);
    scrollPanel.addEventListener("pointerdown", () => { userScrolled = true; }, { passive: true });
    refreshFades = (): void => requestAnimationFrame(updateFades);
  }

  // setGroups can otherwise leave the chart scrolled to the bottom on first render
  // (including from vis-timeline's own rangechanged firing right after construction)
  // — re-pin to the top until the visitor scrolls it themselves.
  function applyVisibleGroups(range: { start: Date; end: Date }): void {
    const ids = visibleGroupIds(items, {
      start: range.start.getTime(),
      end: range.end.getTime(),
    });
    timeline.setGroups(
      groups.map((group) => ({ ...group, visible: ids.has(group.id) })),
    );
    if (scrollPanel && !userScrolled) {
      requestAnimationFrame(() => {
        scrollPanel.scrollTop = 0;
        refreshFades();
      });
    } else {
      refreshFades();
    }
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

  let resizeTimer: number | undefined;
  window.addEventListener("resize", () => {
    window.clearTimeout(resizeTimer);
    resizeTimer = window.setTimeout(() => applyRootHeight(root), 150);
  });
}

main();
