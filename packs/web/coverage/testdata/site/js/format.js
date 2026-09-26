// Formatting for the parcel pages.
function statusLabel(status) {
  switch (status) {
    case "in_transit":
      return "In transit";
    case "delivered":
      return "Delivered";
    case "returned":
      return "Returned to sender";
    default:
      return "Unknown";
  }
}

function formatWeight(kg) {
  if (kg < 1) {
    return Math.round(kg * 1000) + " g";
  }
  return kg.toFixed(1) + " kg";
}
