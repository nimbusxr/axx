import { priceFor, zoneOf } from "./rates";

function show(text: string): void {
  document.getElementById("price")!.textContent = text;
}

function quote(): void {
  const grams = Number((document.getElementById("weight") as HTMLInputElement).value);
  if (!grams || grams <= 0) {
    show("Enter a weight");
    return;
  }
  const zone = zoneOf((document.getElementById("country") as HTMLSelectElement).value);
  show(`Price: ${priceFor(grams, zone).toFixed(2)} EUR`);
}

function cancel(): void {
  if (confirm("Cancel this quote?")) {
    show("Quote cancelled");
  } else {
    show("Quote kept");
  }
}

document.getElementById("quote")!.addEventListener("click", quote);
document.getElementById("cancel")!.addEventListener("click", cancel);
