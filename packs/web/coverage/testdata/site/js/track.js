// Tracking: a parcel's status, by its reference.
const parcels = {
  "PX-1042": { status: "in_transit", weight: 0.4 },
  "PX-2077": { status: "delivered", weight: 12.5 },
  "PX-3310": { status: "returned", weight: 2 },
};

function trackParcel() {
  const reference = document.getElementById("reference").value.trim();
  const parcel = parcels[reference];
  const status = document.getElementById("status");
  if (!parcel) {
    status.textContent = "No parcel " + reference;
    return;
  }
  status.textContent = reference + ": " + statusLabel(parcel.status) + ", " + formatWeight(parcel.weight);
}

function reportProblem(event) {
  event.preventDefault();
  const reason = document.getElementById("reason").value.trim();
  if (reason.length < 5) {
    document.getElementById("status").textContent = "Say what went wrong";
    return;
  }
  location.href = "/claim.html?reason=" + encodeURIComponent(reason);
}
