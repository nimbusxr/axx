export type Zone = "domestic" | "europe" | "world";

export function zoneOf(country: string): Zone {
  switch (country) {
    case "Germany":
      return "domestic";
    case "France":
      return "europe";
    default:
      return "world";
  }
}

const perKilo: Record<Zone, number> = { domestic: 1.2, europe: 2.9, world: 7.5 };

export function priceFor(grams: number, zone: Zone): number {
  const base = zone === "domestic" ? 4.5 : 9;
  return base + Math.ceil(grams / 1000) * perKilo[zone];
}
