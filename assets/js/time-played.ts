// "Time played" sidebar cards (time-played-stat.html). Hovering shows the total in
// every format; clicking cycles the card through them. Every card on the page
// shares one format, which starts at hours on each page load.

interface Format {
  // compact form shown on the card, short enough to share a line with its label
  short: (mins: number) => string;
  long: (mins: number) => string;
}

const HOUR = 60;
const DAY = 24 * HOUR;
const WEEK = 7 * DAY;
const YEAR = 365.25 * DAY;
const MONTH = YEAR / 12;

function num(n: number, digits: number): string {
  return n.toLocaleString("en-US", { minimumFractionDigits: digits, maximumFractionDigits: digits });
}

function plural(n: string, unit: string): string {
  return `${n} ${unit}${n === "1" || n === "1.0" || n === "1.00" ? "" : "s"}`;
}

function breakdown(mins: number, withMinutes: boolean): string {
  const d = Math.floor(mins / DAY);
  const h = Math.floor((mins % DAY) / HOUR);
  const m = Math.round(mins % HOUR);
  return withMinutes ? `${d}d ${h}h ${m}m` : `${d}d ${h}h`;
}

const FORMATS: Format[] = [
  { short: (m) => `${num(m / HOUR, 0)}h`, long: (m) => plural(num(m / HOUR, 0), "hour") },
  { short: (m) => `${num(m / DAY, 1)}d`, long: (m) => plural(num(m / DAY, 1), "day") },
  { short: (m) => `${num(m / WEEK, 1)}w`, long: (m) => plural(num(m / WEEK, 1), "week") },
  { short: (m) => `${num(m / MONTH, 1)}mo`, long: (m) => plural(num(m / MONTH, 1), "month") },
  { short: (m) => `${num(m / YEAR, 2)}y`, long: (m) => plural(num(m / YEAR, 2), "year") },
  // the card drops the minutes to keep the value short
  { short: (m) => breakdown(m, false), long: (m) => breakdown(m, true) },
  { short: (m) => `${num(m / 1000, 0)}k min`, long: (m) => plural(num(m, 0), "minute") },
];

const cards = Array.from(document.querySelectorAll<HTMLButtonElement>("[data-time-played]"));
let index = 0;

function render(): void {
  for (const card of cards) {
    const mins = Number(card.dataset.mins) || 0;
    const value = card.querySelector<HTMLElement>("[data-time-played-value]");
    if (value) value.textContent = FORMATS[index].short(mins);
    card.title = FORMATS.map((f) => f.long(mins)).join("\n") + "\n\nClick to change format";
    card.setAttribute("aria-label", `Time played: ${FORMATS[index].long(mins)}. Click to change format.`);
  }
}

for (const card of cards) {
  card.addEventListener("click", () => {
    index = (index + 1) % FORMATS.length;
    render();
  });
}
render();
