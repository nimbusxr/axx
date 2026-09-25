package main

import "testing"

func TestInvoiceName(t *testing.T) {
	for object, want := range map[string][3]string{
		"kestrel/INV-2026-09-KES.csv": {"KESTREL", "INV-2026-09-KES", "ok"},
		"INV-1.csv":                   {"", "", ""},
		"kestrel/2026/INV-1.csv":      {"", "", ""},
		"kestrel/INV-1.pdf":           {"", "", ""},
	} {
		carrier, invoice, ok := invoiceName(object)
		if carrier != want[0] || invoice != want[1] || ok != (want[2] == "ok") {
			t.Errorf("%s: %s %s %v", object, carrier, invoice, ok)
		}
	}
}

func TestMoney(t *testing.T) {
	if got := money(cents(338)); got != "3.38" {
		t.Errorf("money = %s", got)
	}
}
