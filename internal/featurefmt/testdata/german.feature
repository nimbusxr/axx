# language: de
@paket
Funktionalität: Pakete
  Grundlage:
    Angenommen ein Kurier K-7
  Szenario: Ein Paket wird zugestellt
    Wenn K-7 das Paket PX-1 zustellt
    Dann ist PX-1 ZUGESTELLT
      Und die Tabelle hat eine Zeile:
        |Paket|Status|
        |PX-1|ZUGESTELLT|
