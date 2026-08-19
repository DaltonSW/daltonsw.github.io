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

document.querySelectorAll<HTMLElement>("[data-ach-root]").forEach(initAchievementReveal);
