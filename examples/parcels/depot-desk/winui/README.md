# Depot desk in WinUI 3

The depot desk (`../README.md`) in WinUI 3, unpackaged, with the Windows App SDK in it.

```sh
dotnet build -c Release -p:Platform=x64
bin/x64/Release/net8.0-windows10.0.19041.0/DepotDesk.exe
```

- `EnableMsixTooling` gives `dotnet build` the Windows App SDK's own resource tools, as it has no
  Visual Studio's.
- The expected parcels' rows are built in code: built with `dotnet build`, the XAML compiler
  fails (WMC9999) on a typed data template. WinUI has no table: they are a list's items.
- The signature pad is a panel, which has no automation peer of its own: it gives one, an
  image named Courier signature.
- The desk sizes its window in points, as AppWindow takes pixels. WinUI places a new window
  anywhere, part of it under the taskbar: axx moves it into the screen's work area.

What WinUI gives accessibility: its lists take no wheel turns that `SendInput` makes, but take
UI Automation's Scroll pattern; and a hit test stops at the content's bridge
(`DesktopChildSiteBridge`), above the control there.

The desk keeps its arrivals in `%APPDATA%\Parcels\Depot desk\arrivals.json`, and the service
level in the registry, `HKCU\Software\Parcels\Depot desk`.
