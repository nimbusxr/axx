// What the page keeps, and the menu, through the main process.
const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("depot", {
  load: () => ipcRenderer.invoke("load"),
  saveArrivals: (arrivals) => ipcRenderer.invoke("save-arrivals", arrivals),
  saveLevel: (level) => ipcRenderer.invoke("save-level", level),
  setCloseDayEnabled: (enabled) => ipcRenderer.invoke("close-day-enabled", enabled),
  onCloseDay: (fn) => ipcRenderer.on("close-day", () => fn()),
});
