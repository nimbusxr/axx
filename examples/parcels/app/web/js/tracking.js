// The tracking page: when the parcel arrives, and its depot scans as they happen.
const ref = document.body.dataset.reference;
const $ = (id) => document.getElementById(id);
// When it arrives, in the recipient's own language and time zone.
const day = (t) => new Date(t).toLocaleDateString(undefined, { weekday: 'long', month: 'long', day: 'numeric' });
const time = (t) => new Date(t).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
(async () => {
  let r;
  try {
    r = await fetch('/portal/track/' + encodeURIComponent(ref) + '/estimate');
  } catch {
    $('estimate').textContent = 'No delivery estimate: check your connection';
    return;
  }
  if (!r.ok) {
    $('estimate').textContent = 'No delivery estimate right now';
    return;
  }
  const e = await r.json();
  $('estimate').textContent = e.deliveredAt
    ? 'Delivered on ' + day(e.deliveredAt) + ', at ' + time(e.deliveredAt)
    : 'Arrives on ' + day(e.from) + ', between ' + time(e.from) + ' and ' + time(e.to);
})();
// The parcel's depot scans, as they happen.
const statuses = { REGISTERED: 'Not scanned yet', IN_TRANSIT: 'In transit', OUT_FOR_DELIVERY: 'Out for delivery', DELIVERED: 'Delivered' };
const live = new WebSocket(location.origin.replace(/^http/, 'ws') + '/portal/track/' + encodeURIComponent(ref) + '/live');
live.onopen = () => live.send(JSON.stringify({ follow: ref }));
live.onmessage = (m) => {
  const s = JSON.parse(m.data);
  $('scan').textContent = (statuses[s.status] || s.status) + (s.location ? ' from ' + s.location : '');
};
