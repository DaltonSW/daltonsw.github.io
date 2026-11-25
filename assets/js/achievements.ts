// Click-to-reveal for hidden achievements, plus a per-section "Unhide everything"
// toggle. Rows render blurred and clickable without JS.

function initAchievementReveal(root: HTMLElement): void {
  const rows = Array.from(root.querySelectorAll<HTMLElement>(".ach-row--hidden"));
  if (rows.length === 0) return;

  for (const row of rows) {
    row.addEventListener("click", () => {
      const revealed = row.classList.toggle("ach-row--revealed");
      row.setAttribute("aria-pressed", String(revealed));
      const label = revealed ? "Click to hide again" : "Hidden achievement — click to reveal";
      row.setAttribute("aria-label", label);
      row.title = label;
    });
    row.addEventListener("keydown", (e: KeyboardEvent) => {
      if (e.key !== "Enter" && e.key !== " ") return;
      e.preventDefault();
      row.click();
    });
  }

  const toggle = root.querySelector<HTMLButtonElement>("[data-ach-reveal-toggle]");
  if (!toggle) return;
  toggle.hidden = false;
  toggle.addEventListener("click", () => {
    const active = root.classList.toggle("ach-reveal-all");
    toggle.setAttribute("aria-pressed", String(active));
    toggle.classList.toggle("ach-reveal-toggle--active", active);
    toggle.textContent = active ? "Hide spoilers again" : "Unhide everything";

    // reveal-all is visual only — sync each row's tooltip to what's on screen
    for (const row of rows) {
      const revealed = active || row.classList.contains("ach-row--revealed");
      const label = revealed ? "Click to hide again" : "Hidden achievement — click to reveal";
      row.setAttribute("aria-label", label);
      row.title = label;
    }
  });
}

// Tooltips are set on hover rather than rendered per row, which cost ~740 KB on /games.
function addHoverTitles(): void {
  document.addEventListener("mouseover", (e) => {
    const el = e.target instanceof Element ? e.target : null;
    const desc = el?.closest<HTMLElement>(".ach-row__desc");
    if (desc) {
      const truncated = desc.scrollWidth > desc.clientWidth;
      const hidden = desc.closest(".ach-row--hidden:not(.ach-row--revealed)") && !desc.closest(".ach-reveal-all, .spoilers-shown");
      desc.title = truncated && !hidden ? (desc.textContent ?? "") : "";
      return;
    }
    const rarity = el?.closest<HTMLElement>(".ach-row__rarity");
    if (rarity && !rarity.title) rarity.title = "Percent of players who earned this";
  });
}

document.querySelectorAll<HTMLElement>("[data-ach-root]").forEach(initAchievementReveal);
addHoverTitles();
