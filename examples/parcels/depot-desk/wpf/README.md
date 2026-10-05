# Depot desk in WPF

The depot desk (`../README.md`) in WPF, on .NET 8: its signature pad is WPF's `InkCanvas`, its
expected parcels a `DataGrid`, its rules link a `Hyperlink`.

```sh
dotnet build -c Release
bin/Release/net8.0-windows/DepotDesk.exe
```

Print label is a checkbox (WPF has no switch). The desk keeps its arrivals in
`%APPDATA%\Parcels\Depot desk\arrivals.json`, and the service level in
`%LOCALAPPDATA%\Parcels\Depot desk\settings.json`.
