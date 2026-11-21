// Generic tablist controller for [data-tabs] containers. Panels render visible and
// stacked without JS; the tablist starts `hidden` and is revealed here.

// No safety margin: the measurement is exact now (see applyPanelHeights), and
// any slack here just showed as a band of empty space above the footer.
const PANEL_GAP = 0;
const MIN_PANEL_HEIGHT = 320;

function applyPanelHeights(list: HTMLElement, panels: Map<string, HTMLElement>): void {
  // measure from where the visible panel actually starts — the tablist's own
  // bottom margin sits between the two, and measuring from the list let the
  // panel overshoot the viewport by that much
  const visible = Array.from(panels.values()).find((p) => !p.hidden);
  const top = visible ? visible.getBoundingClientRect().top : list.getBoundingClientRect().bottom;

  const main = document.querySelector<HTMLElement>(".site-main");
  const belowPadding = main ? parseFloat(getComputedStyle(main).paddingBottom) : 0;
  const footer = document.querySelector<HTMLElement>(".site-footer");
  const footerHeight = footer ? footer.getBoundingClientRect().height : 0;

  // floored so a fractional pixel can't tip the page into scrolling
  const height = Math.max(
    Math.floor(window.innerHeight - top - belowPadding - footerHeight - PANEL_GAP),
    MIN_PANEL_HEIGHT,
  );
  for (const panel of panels.values()) {
    if (panel.hasAttribute("data-self-sized")) continue; // manages its own scrolling
    panel.style.maxHeight = `${height}px`;
  }
}

function activate(tabs: HTMLElement[], panels: Map<string, HTMLElement>, tab: HTMLElement): void {
  for (const t of tabs) {
    const selected = t === tab;
    t.setAttribute("aria-selected", String(selected));
    t.tabIndex = selected ? 0 : -1;
    const panel = panels.get(t.id);
    if (panel) panel.hidden = !selected;
  }
}

function initTabs(root: HTMLElement): void {
  const list = root.querySelector<HTMLElement>('[role="tablist"]');
  const tabs = Array.from(root.querySelectorAll<HTMLElement>('[role="tab"]'));
  if (!list || tabs.length === 0) return;

  const panels = new Map<string, HTMLElement>();
  for (const tab of tabs) {
    const panelId = tab.getAttribute("aria-controls");
    const panel = panelId ? document.getElementById(panelId) : null;
    if (panel) panels.set(tab.id, panel);
  }

  const initial = tabs.find((t) => t.getAttribute("aria-selected") === "true") ?? tabs[0];
  activate(tabs, panels, initial);
  list.hidden = false;
  applyPanelHeights(list, panels);
  // The web fonts re-flow the heading and intro above the tabs once they load,
  // which moves the panels down; measured only before that, the page scrolled.
  // fonts.ready alone isn't enough: on a cold load it can resolve before any
  // font has started loading, so also catch each font finishing, and `load`.
  const remeasure = () => applyPanelHeights(list, panels);
  document.fonts?.ready.then(remeasure);
  document.fonts?.addEventListener("loadingdone", remeasure);
  window.addEventListener("load", remeasure);

  let resizeTimer: number | undefined;
  window.addEventListener("resize", () => {
    window.clearTimeout(resizeTimer);
    resizeTimer = window.setTimeout(() => applyPanelHeights(list, panels), 150);
  });

  tabs.forEach((tab, i) => {
    tab.addEventListener("click", () => activate(tabs, panels, tab));

    tab.addEventListener("keydown", (e: KeyboardEvent) => {
      let target: HTMLElement | null = null;
      if (e.key === "ArrowRight") target = tabs[(i + 1) % tabs.length];
      else if (e.key === "ArrowLeft") target = tabs[(i - 1 + tabs.length) % tabs.length];
      else if (e.key === "Home") target = tabs[0];
      else if (e.key === "End") target = tabs[tabs.length - 1];
      if (!target) return;
      e.preventDefault();
      target.focus();
      activate(tabs, panels, target);
    });
  });
}

document.querySelectorAll<HTMLElement>("[data-tabs]").forEach(initTabs);
