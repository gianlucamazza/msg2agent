import { fetchPublicConfig } from "@/lib/api.js";

fetchPublicConfig().then(({ paid_enabled }) => {
  const html = document.documentElement;
  html.dataset.paid = paid_enabled ? "true" : "false";
  if (!paid_enabled) {
    const disclaimer = document.getElementById("plan-disclaimer");
    if (disclaimer) disclaimer.style.display = "";
    document
      .querySelectorAll<HTMLElement>(
        ".plan-card.featured .paid-only, .plan-card:last-child .paid-only",
      )
      .forEach((el) => {
        el.style.display = "none";
      });
    document
      .querySelectorAll<HTMLElement>(
        ".plan-card.featured [aria-disabled], .plan-card:last-child [aria-disabled]",
      )
      .forEach((el) => {
        el.style.display = "";
      });
  }
});
