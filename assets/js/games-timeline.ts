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

// Trailing delay before re-filtering rows, so a pan gesture never reflows mid-swipe.
const REGROUP_DELAY_MS = 200;

// Ceiling on the blank space reserved above the first row (see setPadTop).
const MAX_PAD_TOP = 96;

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
    // item: "top" is load-bearing: the default "bottom" makes vis-timeline shift its own
    // scrollTop by the height delta on every row-set change, which is most of the
    // vertical jumping when panning and silently overrides repin().
    orientation: { axis: "top", item: "top" },
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
      // Scrolling up to the very top is a deliberate "show me the first row", so give
      // back any reserved space rather than leaving a blank strip above it.
      if (event.deltaY < 0 && scrollPanel.scrollTop === 0) setPadTop(0);
      // Scrolling vertically *is* re-aiming — without this, repin() would fight it
      // and snap the view straight back.
      captureAnchor();
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

  // Group ids currently drawn, in render order — .vis-label and .vis-group children
  // line up with this, which is how a row's on-screen position gets measured.
  let appliedOrder: unknown[] = [];
  let hoveredGroupId: unknown = null;
  let rowsLocked = false;

  // Label column and chart lane for one row — same index into both, since
  // vis-timeline renders each in group order.
  const rowsAt = (index: number): HTMLElement[] =>
    [".vis-labelset > .vis-label", ".vis-foreground > .vis-group"]
      .map((selector) => root.querySelectorAll<HTMLElement>(selector)[index])
      .filter((element): element is HTMLElement => Boolean(element));

  // Measured for anchoring: the label column is the side with native scroll.
  const labelAt = (index: number): HTMLElement | undefined =>
    root.querySelectorAll<HTMLElement>(".vis-labelset > .vis-label")[index];

  // Rows that vanish are usually *above* the tracked one (newest-first order, panning
  // backwards), and at scrollTop 0 there's no scroll left to give back — so reserve
  // space above the first row to scroll into. Self-heals once a regroup leaves slack.
  const PAD_TARGETS = [".vis-labelset", ".vis-foreground", ".vis-itemset > .vis-background", ".vis-axis"];
  let padTop = 0;
  function setPadTop(value: number): void {
    padTop = Math.min(Math.max(Math.round(value), 0), MAX_PAD_TOP);
    for (const selector of PAD_TARGETS) {
      const el = root.querySelector<HTMLElement>(selector);
      if (el) el.style.paddingTop = `${padTop}px`;
    }
  }

  // Highlighting the label and the lane together is what makes one game readable
  // straight across the chart.
  function paintHoveredRow(): void {
    for (const stale of root.querySelectorAll(".games-timeline-row--hover")) {
      stale.classList.remove("games-timeline-row--hover");
    }
    const index = appliedOrder.indexOf(hoveredGroupId);
    if (hoveredGroupId === null || index === -1) return;
    for (const row of rowsAt(index)) row.classList.add("games-timeline-row--hover");
  }

  const rowScreenY = (id: unknown): number | null => {
    const label = labelAt(appliedOrder.indexOf(id));
    return label ? label.getBoundingClientRect().top : null;
  };

  // The visitor's frame of reference — the row under the cursor and where it sits now,
  // else whatever's nearest the middle. Re-captured only on pointer movement and
  // vertical scrolling, i.e. "this is where I'm looking"; held across everything else.
  let anchor: { id: unknown; screenY: number } | null = null;
  function captureAnchor(): void {
    let id = hoveredGroupId;
    if (id === null && scrollPanel) {
      const middle = scrollPanel.getBoundingClientRect().top + scrollPanel.clientHeight / 2;
      let bestDistance = Infinity;
      for (const candidate of appliedOrder) {
        const y = rowScreenY(candidate);
        if (y === null) continue;
        const distance = Math.abs(y - middle);
        if (distance < bestDistance) {
          bestDistance = distance;
          id = candidate;
        }
      }
    }
    const screenY = id === null ? null : rowScreenY(id);
    anchor = screenY === null ? null : { id, screenY };
  }

  // Re-pinned after every redraw, not just on regroup: a group's height tracks the items
  // currently in range, so panning alone reflows every row below it.
  let repinning = false;
  function repin(): void {
    if (!scrollPanel || !anchor || repinning) return;
    const current = rowScreenY(anchor.id);
    if (current === null) {
      // The tracked row has panned out of range, so there's nothing left to hold in
      // place — hand back the reserved space now rather than leaving a blank strip.
      anchor = null;
      if (padTop > 0) {
        setPadTop(0);
        scrollPanel.dispatchEvent(new Event("scroll"));
        refreshFades();
      }
      return;
    }
    const delta = anchor.screenY - current;
    if (Math.abs(delta) < 1) return;

    repinning = true;
    let desired = scrollPanel.scrollTop - delta;
    if (desired < 0) {
      setPadTop(padTop - desired);
      desired = 0;
    } else if (padTop > 0) {
      const shrink = Math.min(padTop, desired);
      setPadTop(padTop - shrink);
      desired -= shrink;
    }
    const max = Math.max(scrollPanel.scrollHeight - scrollPanel.clientHeight, 0);
    scrollPanel.scrollTop = Math.min(Math.max(desired, 0), max);
    // Only the label column scrolls natively; vis-timeline drives the lanes off this
    // event, so fire it by hand to keep both columns in step within this frame.
    scrollPanel.dispatchEvent(new Event("scroll"));
    repinning = false;
    refreshFades();
  }

  // setGroups can otherwise leave the chart scrolled to the bottom on first render
  // (including from vis-timeline's own rangechanged firing right after construction)
  // — re-pin to the top until the visitor scrolls it themselves.
  function applyVisibleGroups(range: { start: Date; end: Date }): void {
    if (rowsLocked) return;
    const ids = visibleGroupIds(items, {
      start: range.start.getTime(),
      end: range.end.getTime(),
    });

    const nextOrder = groups.map((group) => group.id).filter((id) => ids.has(id));
    // Panning within a stretch where the same games are active changes nothing —
    // skip the re-render rather than tearing the chart down and rebuilding it.
    if (
      nextOrder.length === appliedOrder.length &&
      nextOrder.every((id, index) => id === appliedOrder[index])
    ) {
      return;
    }

    // Anchoring happens in repin(), driven off the redraw below, so it covers row
    // heights changing as well as rows coming and going.
    const hadAnchor = anchor !== null;
    timeline.setGroups(
      groups.map((group) => ({ ...group, visible: ids.has(group.id) })),
    );
    appliedOrder = nextOrder;
    // Forces the layout synchronously; vis-timeline would otherwise redraw a frame later.
    timeline.redraw();
    paintHoveredRow();
    repin();
    // So keyboard/toolbar-driven panning has something to hold onto before the
    // visitor has ever moved the pointer over the chart.
    if (!anchor) captureAnchor();

    // Only when there was no row to anchor to (first render) — otherwise this would
    // undo the restore above.
    if (scrollPanel && !userScrolled && !hadAnchor) {
      requestAnimationFrame(() => {
        scrollPanel.scrollTop = 0;
        refreshFades();
      });
    } else {
      refreshFades();
    }
  }

  applyVisibleGroups({ start: new Date(initialStart), end: new Date(initialEnd) });

  // Every wheel-pan setWindow() emits rangechanged, so filtering on it directly reflowed
  // rows dozens of times per swipe. Trailing-only, so the set settles once panning stops.
  let regroupTimer: number | undefined;
  timeline.on("rangechanged", () => {
    window.clearTimeout(regroupTimer);
    regroupTimer = window.setTimeout(
      () => applyVisibleGroups(timeline.getWindow()),
      REGROUP_DELAY_MS,
    );
  });

  // mousemove, not vis-timeline's mouseOver: mouseOver also fires when a regroup slides
  // a different row under a stationary cursor, which kept re-pointing the anchor at
  // whatever had just moved underneath. A wheel-pan moves no pointer, so this holds.
  root.addEventListener("mousemove", (event: MouseEvent) => {
    const group = timeline.getEventProperties(event).group ?? null;
    if (group !== hoveredGroupId) {
      hoveredGroupId = group;
      paintHoveredRow();
    }
    // Pointer movement is the visitor re-aiming, so the anchor follows it.
    captureAnchor();
  });

  // Anything vis-timeline redraws — panning, zooming, a regroup — can move rows.
  timeline.on("changed", repin);
  root.addEventListener("mouseleave", () => {
    hoveredGroupId = null;
    paintHoveredRow();
  });

  const lockBtn = document.getElementById("games-timeline-lock");
  if (lockBtn) {
    lockBtn.hidden = false;
    lockBtn.addEventListener("click", () => {
      rowsLocked = !rowsLocked;
      lockBtn.setAttribute("aria-pressed", String(rowsLocked));
      lockBtn.classList.toggle("games-timeline__toolbar-btn--active", rowsLocked);
      if (!rowsLocked) applyVisibleGroups(timeline.getWindow());
    });
  }

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
