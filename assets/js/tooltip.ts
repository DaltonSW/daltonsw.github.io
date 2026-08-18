// Custom tooltip for [data-tooltip] elements — replaces the native `title`
// attribute tooltip (unstyleable, inconsistent across browsers, slow to
// appear) with one styled to match the site. A single tooltip element is
// reused for every trigger rather than one per element.

const SHOW_DELAY = 200;
const VIEWPORT_MARGIN = 8;

let tip: HTMLDivElement | undefined;
let showTimer: number | undefined;
let current: HTMLElement | undefined;

function ensureTip(): HTMLDivElement {
  if (tip) return tip;
  tip = document.createElement("div");
  tip.className = "tooltip";
  tip.setAttribute("role", "tooltip");
  tip.hidden = true;
  document.body.appendChild(tip);
  return tip;
}

// Centered above the trigger, flipping below when there isn't room, and
// clamped horizontally so it never runs off the viewport edge.
function position(target: HTMLElement, el: HTMLDivElement): void {
  const rect = target.getBoundingClientRect();
  const tipRect = el.getBoundingClientRect();

  let left = rect.left + rect.width / 2 - tipRect.width / 2;
  left = Math.max(VIEWPORT_MARGIN, Math.min(left, window.innerWidth - tipRect.width - VIEWPORT_MARGIN));

  let top = rect.top - tipRect.height - VIEWPORT_MARGIN;
  if (top < VIEWPORT_MARGIN) top = rect.bottom + VIEWPORT_MARGIN;

  el.style.left = `${left + window.scrollX}px`;
  el.style.top = `${top + window.scrollY}px`;
}

function show(target: HTMLElement): void {
  const text = target.getAttribute("data-tooltip");
  if (!text) return;
  const el = ensureTip();
  el.textContent = text;
  el.hidden = false;
  current = target;
  position(target, el);
}

function hide(): void {
  window.clearTimeout(showTimer);
  if (tip) tip.hidden = true;
  current = undefined;
}

function bind(el: HTMLElement): void {
  el.addEventListener("mouseenter", () => {
    window.clearTimeout(showTimer);
    showTimer = window.setTimeout(() => show(el), SHOW_DELAY);
  });
  el.addEventListener("mouseleave", hide);
  el.addEventListener("focus", () => show(el));
  el.addEventListener("blur", hide);
}

document.querySelectorAll<HTMLElement>("[data-tooltip]").forEach(bind);

// Reposition (rather than hide) on scroll so the tooltip tracks a cell in a
// scrolling table instead of visibly detaching from it.
window.addEventListener(
  "scroll",
  () => {
    if (current && tip && !tip.hidden) position(current, tip);
  },
  { passive: true, capture: true },
);
