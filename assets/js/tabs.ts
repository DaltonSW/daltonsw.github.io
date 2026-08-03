// Generic tablist controller for [data-tabs] containers, e.g. the games taxonomy page.
// Panels render visible and stacked without JS (see taxonomy.html); this is what turns
// them into real tabs, so the tablist itself starts `hidden` and is revealed here.

// Breathing room below the panel; generous since font-metric rounding can otherwise
// leave a stray page scrollbar with nothing to scroll.
const PANEL_GAP = 32;

// Floor so a panel doesn't collapse to nothing on very short viewports.
const MIN_PANEL_HEIGHT = 320;

// Caps each panel to the viewport space below the tab strip so it scrolls
// internally instead of stretching the page.
function applyPanelHeights(list: HTMLElement, panels: Map<string, HTMLElement>): void {
  const top = list.getBoundingClientRect().bottom;

  const main = document.querySelector<HTMLElement>(".site-main");
  const belowPadding = main ? parseFloat(getComputedStyle(main).paddingBottom) : 0;
  const footer = document.querySelector<HTMLElement>(".site-footer");
  const footerHeight = footer ? footer.getBoundingClientRect().height : 0;

  const height = Math.max(
    window.innerHeight - top - belowPadding - footerHeight - PANEL_GAP,
    MIN_PANEL_HEIGHT,
  );
  for (const panel of panels.values()) {
    // Self-sized panels (the timeline) manage their own height and scrolling.
    if (panel.hasAttribute("data-self-sized")) continue;
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
