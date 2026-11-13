export type Event = { parcel: string; status: string; at: string };

const events = new Map<string, Event[]>();

export function record(parcel: string, status: string): Event {
  if (!["picked-up", "in-transit", "delivered"].includes(status)) {
    throw new Error(`no such status: ${status}`);
  }
  const e = { parcel, status, at: new Date().toISOString() };
  events.set(parcel, [...(events.get(parcel) ?? []), e]);
  return e;
}

export function history(parcel: string): Event[] {
  return events.get(parcel) ?? [];
}
