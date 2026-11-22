// Log tab filters: entry type, game title, platform, and achievement source.
// Events carry data-kind/title/platform/provider (games-log.html). Days and year
// groups with nothing left hide. While a filter is active, the matching groups are
// forced open; clearing every filter restores each group's original state.

function initGamesLog(root: HTMLElement): void {
  const filters = root.querySelector<HTMLElement>("[data-log-filters]");
  if (!filters) return;

  const search = root.querySelector<HTMLInputElement>("[data-log-search]");
  const platform = root.querySelector<HTMLSelectElement>("[data-log-platform]");
  const provider = root.querySelector<HTMLSelectElement>("[data-log-provider]");
  const kinds = Array.from(root.querySelectorAll<HTMLInputElement>("[data-log-kind]"));
  const empty = root.querySelector<HTMLElement>("[data-log-empty]");
  const groups = Array.from(root.querySelectorAll<HTMLDetailsElement>(".ach-list > .year-group"));

  const originalOpen = new Map<HTMLDetailsElement, boolean>();
  for (const group of groups) originalOpen.set(group, group.open);

  function apply(): void {
    const query = (search?.value ?? "").trim().toLowerCase();
    const wantedPlatform = platform?.value ?? "";
    const wantedProvider = provider?.value ?? "";
    const enabled = new Set(kinds.filter((k) => k.checked).map((k) => k.dataset.logKind));
    const active =
      query !== "" || wantedPlatform !== "" || wantedProvider !== "" || enabled.size < kinds.length;

    let anyMatch = false;
    for (const group of groups) {
      for (const ev of Array.from(group.querySelectorAll<HTMLElement>(".log-event"))) {
        const d = ev.dataset;
        // an achievement source only applies to achievements, so picking one hides playthroughs
        const matches =
          enabled.has(d.kind) &&
          (query === "" || (d.title ?? "").includes(query)) &&
          (wantedPlatform === "" || d.platform === wantedPlatform) &&
          (wantedProvider === "" || d.provider === wantedProvider);
        ev.hidden = !matches;
      }
      // a day hides once all of its events do; a year once all of its days do
      let groupHasMatch = false;
      for (const day of Array.from(group.querySelectorAll<HTMLElement>(".log-day"))) {
        day.hidden = !day.querySelector(".log-event:not([hidden])");
        if (!day.hidden) groupHasMatch = true;
      }
      group.hidden = !groupHasMatch;
      if (groupHasMatch) anyMatch = true;
      group.open = active ? groupHasMatch : (originalOpen.get(group) ?? group.open);
    }
    if (empty) empty.hidden = anyMatch;
  }

  search?.addEventListener("input", apply);
  platform?.addEventListener("change", apply);
  provider?.addEventListener("change", apply);
  for (const k of kinds) k.addEventListener("change", apply);
  filters.hidden = false;
}

document.querySelectorAll<HTMLElement>("[data-log-root]").forEach(initGamesLog);
