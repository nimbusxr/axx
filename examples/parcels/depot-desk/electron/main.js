// The depot desk in Electron (../README.md): the page in ../web, what it
// keeps in the app's userData folder, and the Depot menu.
const { app, BrowserWindow, Menu, ipcMain } = require("electron");
const fs = require("node:fs/promises");
const path = require("node:path");

const file = (name) => path.join(app.getPath("userData"), name);

async function read(name, otherwise) {
  try {
    return JSON.parse(await fs.readFile(file(name), "utf8"));
  } catch {
    return otherwise;
  }
}

async function write(name, value) {
  await fs.mkdir(app.getPath("userData"), { recursive: true });
  await fs.writeFile(file(name), JSON.stringify(value));
}

let window;

function menu() {
  const template = [
    ...(process.platform === "darwin" ? [{ role: "appMenu" }] : []),
    {
      label: "Depot",
      submenu: [{ id: "close-day", label: "Close day", enabled: false, click: () => window.webContents.send("close-day") }],
    },
  ];
  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}

app.whenReady().then(() => {
  ipcMain.handle("load", async () => ({
    arrivals: await read("arrivals.json", []),
    level: (await read("settings.json", {})).serviceLevel,
  }));
  ipcMain.handle("save-arrivals", (_, arrivals) => write("arrivals.json", arrivals));
  ipcMain.handle("save-level", async (_, level) => write("settings.json", { ...(await read("settings.json", {})), serviceLevel: level }));
  ipcMain.handle("close-day-enabled", (_, enabled) => {
    Menu.getApplicationMenu().getMenuItemById("close-day").enabled = enabled;
  });
  menu();
  window = new BrowserWindow({
    width: 640,
    height: 580,
    title: "Depot desk",
    webPreferences: { preload: path.join(__dirname, "preload.js") },
  });
  window.loadFile(path.join(__dirname, "..", "web", "index.html"));
});

app.on("window-all-closed", () => app.quit());
