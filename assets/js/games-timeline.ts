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

const NEARBY_PADDING_RATIO = 0.5;
const MAX_PADDING_DAYS = 60; // cap on the ratio-based padding when zoomed way out
const DEFAULT_WINDOW_DAYS = 180;
const REGROUP_DELAY_MS = 200; // trailing delay so a pan gesture never reflows mid-swipe
const CONNECTOR_GAP_DAYS = 180; // gap beyond which sessions no longer bridge into one bar
const NO_CONNECTOR_CLASSES = ["pt--endless", "pt--multiplayer", "pt--software"];

function formatMonthYear(value: string | number | Date): string {
  return new Date(value).toLocaleDateString("en-US", { month: "short", year: "numeric" });
}

function buildConnectors(items: GameItem[], groupNames: Map<string, string>): GameItem[] {
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
      const name = groupNames.get(clusterStart.group as string) ?? "";
      const span = `${formatMonthYear(clusterStart.start)} – ${formatMonthYear(last.end as string)}`;
      connectors.push({
        id: `${key}--connector--${clusterStart.id}`,
        group: clusterStart.group,
        subgroup: clusterStart.subgroup,
        type: "range",
        className: (clusterStart.className ?? "").replace("pt-session", "pt-connector"),
        content: "",
        title: name ? `${name}<br>Dormant ${span}` : `Dormant ${span}`,
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

const HEIGHT_GAP = 32;
const MIN_ROOT_HEIGHT = 240;

function applyRootHeight(root: HTMLElement): void {
  const top = root.getBoundingClientRect().top;

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

  // deferred a frame so tabs.ts has already revealed the tab strip
  requestAnimationFrame(() => renderTimeline(root, groups, items));
}

function renderTimeline(root: HTMLElement, groups: DataGroup[], items: GameItem[]): void {
  const today = new Date().toISOString().slice(0, 10);
  for (const item of items) {
    if (!item.end) item.end = today;
  }

  const groupNames = new Map<string, string>();
  for (const group of groups) {
    const name = (group.content ?? "").replace(/<[^>]*>/g, "");
    groupNames.set(group.id as string, name);
  }

  items.push(...buildConnectors(items, groupNames));

  const domainStart = Math.min(...items.map((item) => toTime(item.start)));
  const domainEnd = Math.max(...items.map((item) => toTime(item.end as string)));
  const domainSpan = Math.max(domainEnd - domainStart, daySpan * 30);

  root.replaceChildren();
  applyRootHeight(root);

  // must be set via options.start/end — a post-construction setWindow() gets overridden by vis-timeline's initial auto-fit
  const initialEnd = domainEnd;
  const initialStart = Math.max(domainStart, initialEnd - daySpan * DEFAULT_WINDOW_DAYS);

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
    height: "100%",
    verticalScroll: true,
    horizontalScroll: true,
    locale: "en",
    // item: "top" avoids vis-timeline shifting its own scrollTop on every row-set change
    orientation: { axis: "top", item: "top" },
    tooltip: {
      followMouse: true,
      overflowMethod: "cap",
    },
    template: (item: GameItem) =>
      item.className?.includes("pt-connector") ? "" : (item.status ?? ""),
  });

  // fade top/bottom edges in when there's more to scroll; .vis-panel.vis-left is the
  // panel vis-timeline gives a real scrollbar (.vis-center just mirrors it)
  const scrollPanel = root.querySelector<HTMLElement>(".vis-panel.vis-left");
  const axisPanel = root.querySelector<HTMLElement>(".vis-panel.vis-top");

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
    refreshFades = (): void => requestAnimationFrame(updateFades);
  }

  let appliedOrder: unknown[] = [];
  let hoveredGroupId: unknown = null;
  let rowsLocked = false;

  const rowsAt = (index: number): HTMLElement[] =>
    [".vis-labelset > .vis-label", ".vis-foreground > .vis-group"]
      .map((selector) => root.querySelectorAll<HTMLElement>(selector)[index])
      .filter((element): element is HTMLElement => Boolean(element));

  function paintHoveredRow(): void {
    for (const stale of root.querySelectorAll(".games-timeline-row--hover")) {
      stale.classList.remove("games-timeline-row--hover");
    }
    const index = appliedOrder.indexOf(hoveredGroupId);
    if (hoveredGroupId === null || index === -1) return;
    for (const row of rowsAt(index)) row.classList.add("games-timeline-row--hover");
  }

  // avoids setGroups leaving the chart scrolled to the bottom on first render
  let isInitial = true;
  function applyVisibleGroups(range: { start: Date; end: Date }): void {
    if (rowsLocked) return;
    const ids = visibleGroupIds(items, {
      start: range.start.getTime(),
      end: range.end.getTime(),
    });

    const nextOrder = groups.map((group) => group.id).filter((id) => ids.has(id));
    if (
      nextOrder.length === appliedOrder.length &&
      nextOrder.every((id, index) => id === appliedOrder[index])
    ) {
      return;
    }

    timeline.setGroups(
      groups.map((group) => ({ ...group, visible: ids.has(group.id) })),
    );
    appliedOrder = nextOrder;
    timeline.redraw(); // forces layout synchronously instead of a frame later
    paintHoveredRow();
    if (isInitial && scrollPanel) {
      isInitial = false;
      requestAnimationFrame(() => {
        scrollPanel.scrollTop = 0;
        refreshFades();
      });
    } else {
      refreshFades();
    }
  }

  applyVisibleGroups({ start: new Date(initialStart), end: new Date(initialEnd) });

  // trailing-only so the set settles once panning stops, not on every rangechanged
  let regroupTimer: number | undefined;
  timeline.on("rangechanged", () => {
    window.clearTimeout(regroupTimer);
    regroupTimer = window.setTimeout(
      () => applyVisibleGroups(timeline.getWindow()),
      REGROUP_DELAY_MS,
    );
  });

  // fallback for browsers that route horizontal wheel into a zoom path despite zoomable:false
  root.addEventListener(
    "wheel",
    (event: WheelEvent) => {
      if (Math.abs(event.deltaX) <= Math.abs(event.deltaY)) return;
      event.preventDefault();
      const { start, end } = timeline.getWindow();
      const span = end.getTime() - start.getTime();
      const diff = (event.deltaX * span) / 2400; // same divisor vis-timeline uses internally
      timeline.setWindow(
        new Date(start.getTime() + diff),
        new Date(end.getTime() + diff),
        { animation: false },
      );
    },
    { passive: false },
  );

  root.addEventListener("keydown", (event: KeyboardEvent) => {
    if (event.key === "ArrowLeft" || event.key === "ArrowRight") {
      event.preventDefault();
      const { start, end } = timeline.getWindow();
      const span = end.getTime() - start.getTime();
      const step = span * 0.2 * (event.key === "ArrowLeft" ? -1 : 1);
      timeline.setWindow(
        new Date(start.getTime() + step),
        new Date(end.getTime() + step),
        { animation: false },
      );
    }
  });

  // mousemove, not vis-timeline's mouseOver: mouseOver also fires on regroup, re-pointing
  // the hover target at whatever slid under a stationary cursor
  root.addEventListener("mousemove", (event: MouseEvent) => {
    const group = timeline.getEventProperties(event).group ?? null;
    if (group !== hoveredGroupId) {
      hoveredGroupId = group;
      paintHoveredRow();
    }
  });

  timeline.on("changed", refreshFades);
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

  root.tabIndex = 0; // so tabbing in enables keyboard panning

  let resizeTimer: number | undefined;
  window.addEventListener("resize", () => {
    window.clearTimeout(resizeTimer);
    resizeTimer = window.setTimeout(() => applyRootHeight(root), 150);
  });
}

main();
