// Header search box: fetches /search-index.json (built by
// layouts/index.searchindex.json) on first use and filters it client-side
// as the user types, rendering a dropdown of matching game/project pages.

interface SearchEntry {
  title: string;
  url: string;
  kind: "game" | "project";
  meta: string;
}

const MAX_RESULTS = 8;

function initSiteSearch(root: HTMLElement): void {
  const indexURL = root.dataset.siteSearchIndex;
  const input = root.querySelector<HTMLInputElement>("[data-site-search-input]");
  const results = root.querySelector<HTMLUListElement>("[data-site-search-results]");
  if (!indexURL || !input || !results) return;

  let entries: SearchEntry[] | null = null;
  let loading: Promise<SearchEntry[]> | null = null;
  let activeIndex = -1;

  function loadEntries(): Promise<SearchEntry[]> {
    if (entries) return Promise.resolve(entries);
    if (!loading) {
      loading = fetch(indexURL as string)
        .then((res) => res.json())
        .then((data: SearchEntry[]) => (entries = data))
        .catch(() => (entries = []));
    }
    return loading;
  }

  function close(): void {
    results!.hidden = true;
    results!.innerHTML = "";
    activeIndex = -1;
  }

  function render(matches: SearchEntry[]): void {
    results!.innerHTML = "";
    activeIndex = -1;

    if (matches.length === 0) {
      results!.hidden = true;
      return;
    }

    for (const entry of matches) {
      const li = document.createElement("li");
      li.className = "site-search__result";

      const a = document.createElement("a");
      a.href = entry.url;
      a.className = "site-search__result-link";

      const title = document.createElement("span");
      title.className = "site-search__result-title";
      title.textContent = entry.title;
      a.appendChild(title);

      const kind = document.createElement("span");
      kind.className = "site-search__result-kind";
      kind.textContent = entry.meta ? `${entry.kind} · ${entry.meta}` : entry.kind;
      a.appendChild(kind);

      li.appendChild(a);
      results!.appendChild(li);
    }

    results!.hidden = false;
  }

  function applyFilter(): void {
    const query = input!.value.trim().toLowerCase();
    if (query === "") {
      close();
      return;
    }

    loadEntries().then((all) => {
      const matches = all
        .filter((entry) => entry.title.toLowerCase().includes(query))
        .slice(0, MAX_RESULTS);
      render(matches);
    });
  }

  function moveActive(delta: number): void {
    const links = Array.from(results!.querySelectorAll<HTMLAnchorElement>(".site-search__result-link"));
    if (links.length === 0) return;

    activeIndex = (activeIndex + delta + links.length) % links.length;
    links.forEach((link, i) => link.classList.toggle("is-active", i === activeIndex));
    links[activeIndex].scrollIntoView({ block: "nearest" });
  }

  input.addEventListener("focus", () => void loadEntries());
  input.addEventListener("input", applyFilter);

  input.addEventListener("keydown", (event) => {
    if (results!.hidden) return;

    if (event.key === "ArrowDown") {
      event.preventDefault();
      moveActive(1);
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      moveActive(-1);
    } else if (event.key === "Enter" && activeIndex >= 0) {
      event.preventDefault();
      const links = results!.querySelectorAll<HTMLAnchorElement>(".site-search__result-link");
      links[activeIndex]?.click();
    } else if (event.key === "Escape") {
      close();
      input.blur();
    }
  });

  document.addEventListener("click", (event) => {
    if (!root.contains(event.target as Node)) close();
  });
}

document
  .querySelectorAll<HTMLElement>("[data-site-search]")
  .forEach(initSiteSearch);
