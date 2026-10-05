# Depot desk in Windows Forms

The depot desk (`../README.md`) in Windows Forms, on .NET 8.

```sh
dotnet build -c Release
bin/Release/net8.0-windows/DepotDesk.exe
```

Print label is a checkbox (Windows Forms has no switch). The desk keeps its arrivals in
`%APPDATA%\Parcels\Depot desk\arrivals.json`, and the service level in the registry,
`HKCU\Software\Parcels\Depot desk`.
