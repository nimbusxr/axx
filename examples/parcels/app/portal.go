package main

import (
	"bytes"
	"embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// The shop portal: the web pages shops use to get a price, register parcels
// and see their parcels. Registrations go through the same rules as the API
// (register), with "portal" as their source.

var countries = []struct{ Code, Name string }{
	{"DE", "Germany"}, {"AT", "Austria"}, {"FR", "France"}, {"NL", "Netherlands"},
}

var portalLayout = template.Must(template.New("layout").Funcs(template.FuncMap{
	"countries": func() any { return countries },
	"euros":     func(cents int) string { return fmt.Sprintf("%.2f EUR", float64(cents)/100) },
	"weekdays":  func() []string { return []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday"} },
	"kilos":     func(grams int) string { return fmt.Sprintf("%.1f kg", float64(grams)/1000) },
	"has":       slices.Contains[[]string, string],
	"yesno": func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	},
}).Parse(`{{define "layout"}}<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="icon" href="/portal/icon.svg" type="image/svg+xml">
  <title>{{.Title}} · Parcels</title>
  <meta name="description" content="The Parcels shop portal: get a price, register parcels and follow them to their recipients.">
  <style>
    body { font: 16px/1.5 sans-serif; margin: 0 24px 24px; color: #1d2433; }
    header { display: flex; gap: 16px; align-items: center; padding: 12px 0; border-bottom: 1px solid #d5dbe5; }
    #menu { display: none; }
    .tip { display: none; }
    [aria-label^="About"]:hover + .tip, [aria-label^="About"]:focus + .tip { display: inline; }
    [role=menu] { list-style: none; padding: 4px; border: 1px solid #d5dbe5; width: max-content; }
    [role=menu] > li > * { display: block; padding: 4px 8px; border: 0; background: none; font: inherit; text-align: left; }
    @media (max-width: 600px) {
      #menu { display: inline-block; }
      #links { display: none; }
      #links.open { display: flex; flex-direction: column; }
    }
    @media (prefers-color-scheme: dark) {
      body { background: #111827; color: #e5e7eb; }
      a { color: #93c5fd; }
      header, [role=menu] { border-color: #374151; }
    }
    td, th { padding: 8px 10px; text-align: left; }
    .slots { display: flex; gap: 16px; }
    .slot { min-width: 160px; min-height: 60px; border: 1px dashed #8a94a6; padding: 8px; }
    .chip { display: inline-block; margin: 2px; padding: 2px 6px; border: 1px solid #8a94a6; cursor: grab; }
    /* A cutout on a checkerboard, as image editors show what is transparent. */
    #contents-cutout { background: repeating-conic-gradient(#d5dbe5 0% 25%, #fff 0% 50%) 0 0 / 16px 16px; }
    /* On paper, the page without the portal's menus and buttons. */
    @media print {
      header, [aria-haspopup=menu], [role=tablist], .screen-only { display: none; }
    }
  </style>
</head>
<body>
  <header>
    <strong>Parcels</strong>
    <button id="menu" aria-expanded="false" aria-controls="links">Menu</button>
    <nav id="links"><a href="/portal/quote">Get a quote</a> <a href="/portal/parcels/new">Register a parcel</a></nav>
  </header>
  <p id="offline" role="alert" hidden>You are offline: nothing is saved until you are back online</p>
  <p id="idle" role="alert" hidden>Your session ended after 30 minutes without activity: reload the page to go on</p>
  <main>
  <h1>{{.Title}}</h1>
  {{if .Error}}<p role="alert">{{.Error}}</p>{{end}}
  {{if .Notice}}<p role="status">{{.Notice}}</p>{{end}}
  {{template "body" .}}
  </main>
  <script>
    // On a phone, the links fold into a menu.
    document.getElementById('menu').addEventListener('click', (e) => {
      const open = document.getElementById('links').classList.toggle('open');
      e.target.setAttribute('aria-expanded', String(open));
    });
    addEventListener('offline', () => { document.getElementById('offline').hidden = false; });
    addEventListener('online', () => { document.getElementById('offline').hidden = true; });
    // A session ends after 30 minutes without activity.
    let idle;
    const active = () => {
      clearTimeout(idle);
      idle = setTimeout(() => { document.getElementById('idle').hidden = false; }, 30 * 60 * 1000);
    };
    for (const e of ['keydown', 'pointerdown']) addEventListener(e, active);
    active();
    // Times show in the browser's own language and time zone.
    for (const t of document.querySelectorAll('time[datetime]')) {
      t.textContent = new Date(t.dateTime).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
    }
  </script>
</body>
</html>{{end}}

{{define "country"}}
  <label for="country">Destination country</label>
  <select id="country" name="country">
    <option value="">Choose a country</option>
    {{range countries}}<option value="{{.Code}}"{{if eq .Code $.Form.Country}} selected{{end}}>{{.Name}}</option>{{end}}
  </select>
{{end}}

{{define "service"}}
  <fieldset>
    <legend>Service</legend>
    <label><input type="radio" name="service" value="STANDARD"{{if ne .Form.Service "EXPRESS"}} checked{{end}}> Standard</label>
    <label><input type="radio" name="service" value="EXPRESS"{{if eq .Form.Service "EXPRESS"}} checked{{end}}> Express</label>
    <button type="button" aria-label="About Express">ⓘ</button><span class="tip" role="tooltip">Delivered the next working day within Germany</span>
  </fieldset>
{{end}}`))

// portalPages are the pages, each with its own copy of the layout (every
// page defines its own "body").
var portalPages = map[string]*template.Template{}

func page(name, text string) {
	portalPages[name] = template.Must(template.Must(portalLayout.Clone()).New(name).Parse(text))
}

func init() {
	page("quote", `{{define "body"}}
<form method="post" action="/portal/quote">
  <p><label for="weight">Weight (grams)</label> <input id="weight" name="weight" type="number" value="{{.Form.Weight}}" autofocus></p>
  <p>{{template "country" .}}</p>
  <p><label for="postcode">Postcode</label> <input id="postcode" name="postcode" value="{{.Form.Postcode}}"></p>
  {{template "service" .}}
  <button type="submit">Get a quote</button>
</form>
{{with .Quote}}<p role="status" data-testid="quote">Price: {{euros .PriceCents}} · delivered in {{.DeliveryDays}} {{if eq .DeliveryDays 1}}day{{else}}days{{end}}</p>{{end}}
<p id="pickup"></p>
<script>
  // Parcels registered before 14:00, the shop's time, are picked up the same day.
  document.getElementById('pickup').textContent = new Date().getHours() < 14
    ? 'Registered before 14:00, a parcel is picked up today'
    : 'A parcel registered now is picked up tomorrow';
</script>
{{end}}{{template "layout" .}}`)
	page("register", `{{define "body"}}
<form method="post" action="/portal/parcels" enctype="multipart/form-data">
  <p><label for="sender">Shop account</label> <input id="sender" name="sender" value="{{.Form.Sender}}"></p>
  <p><label for="reference">Parcel reference</label> <input id="reference" name="reference" value="{{.Form.Reference}}"></p>
  <p><label for="weight">Weight (grams)</label> <input id="weight" name="weight" type="number" value="{{.Form.Weight}}"></p>
  {{template "service" .}}
  <fieldset>
    <legend>Recipient</legend>
    <p class="screen-only"><label for="pasted">The customer's address, as they wrote it</label> <textarea id="pasted" rows="2"></textarea> <button type="button" id="fill-address" hidden>Fill in the address</button> <span id="filling" role="status"></span></p>
    <p><label for="name">Recipient name</label> <input id="name" name="name" value="{{.Form.Name}}"></p>
    <p><label for="street">Street</label> <input id="street" name="street" value="{{.Form.Street}}"></p>
    <p><label for="postcode">Postcode</label> <input id="postcode" name="postcode" value="{{.Form.Postcode}}"></p>
    <p><label for="city">City</label> <input id="city" name="city" value="{{.Form.City}}"></p>
    <p>{{template "country" .}}</p>
  </fieldset>
  <fieldset>
    <legend>Delivery</legend>
    <p><label><input type="checkbox" name="signature"{{if .Form.Signature}} checked{{end}}> Signature on delivery</label></p>
    <p><label><input type="checkbox" name="neighbour"{{if .Form.Neighbour}} checked{{end}}> Leave with a neighbour</label></p>
    <p><label for="note">Delivery note</label> <textarea id="note" name="note" rows="2" maxlength="500">{{.Form.Note}}</textarea></p>
    <p><label for="invoice">Customs invoice</label> <input id="invoice" name="invoice" type="file" accept="application/pdf"></p>
    <p><label for="contents">Contents</label> <textarea id="contents" name="contents" rows="2">{{.Form.Contents}}</textarea></p>
    {{if .Customs}}<p><label for="contents-photo">Photo of the contents</label> <input id="contents-photo" type="file" accept="image/png, image/jpeg"> <span id="cutting" role="status"></span></p>
    <p><img id="contents-cutout" alt="The contents, cut out" width="240" hidden><input id="contents-cutout-file" name="contents-photo" type="file" hidden></p>{{end}}
    {{if .Customs}}<p><button type="button" id="describe">Describe for customs</button> <span id="describing" role="status"></span></p>{{end}}
    <p><label for="customs">Customs description</label> <input id="customs" name="customs" value="{{.Form.Customs}}"></p>
  </fieldset>
  <button type="submit" id="register"{{if not .Form.Ready}} disabled{{end}}>Register the parcel</button>
</form>
<script>
  // The parcel needs a shop account and a reference before it can be registered.
  const ready = () => {
    document.getElementById('register').disabled =
      !document.getElementById('sender').value || !document.getElementById('reference').value;
  };
  document.getElementById('sender').addEventListener('input', ready);
  document.getElementById('reference').addEventListener('input', ready);
</script>
{{if .Customs}}<script type="module" src="/portal/js/customs.js"></script>
<script type="module" src="/portal/js/cutout.js"></script>{{end}}
<script type="module" src="/portal/js/address.js"></script>
{{end}}{{template "layout" .}}`)
	page("parcel", `{{define "body"}}
{{with .Parcel}}
<p>Registered: <time datetime="{{.CreatedAt.UTC.Format "2006-01-02T15:04:05Z"}}">{{.CreatedAt.UTC.Format "2006-01-02 15:04"}} UTC</time></p>
<p>
  <button id="actions" aria-haspopup="menu" aria-expanded="false" aria-controls="actions-menu">Actions</button>
  <a href="/portal/track/{{.Reference}}" target="_blank">Track this parcel</a>
</p>
<ul id="actions-menu" role="menu" hidden>
  <li><a role="menuitem" href="/portal/parcels/{{.Reference}}/label.zpl" download>Download the label</a></li>
  <li><button role="menuitem" id="report">Report a problem</button></li>
  {{if eq .Status "REGISTERED"}}<li><button role="menuitem" id="cancel">Cancel the parcel</button></li>{{end}}
</ul>
<form id="cancel-form" method="post" action="/portal/parcels/{{.Reference}}/cancel"></form>
<form id="report-form" method="post" action="/portal/parcels/{{.Reference}}/problems"><input type="hidden" name="note"></form>
<div role="tablist">
  <button role="tab" id="details-tab" aria-selected="true" aria-controls="details">Details</button>
  <button role="tab" id="history-tab" aria-selected="false" aria-controls="history">History</button>
</div>
<section id="details" role="tabpanel" aria-labelledby="details-tab">
<dl>
  <dt>Recipient</dt><dd>{{.Recipient.Name}}, {{.Recipient.Street}}, {{.Recipient.Postcode}} {{.Recipient.City}}</dd>
  <dt>Service</dt><dd>{{.ServiceLevel}}</dd>
  <dt>Zone</dt><dd>{{.Zone}}</dd>
  <dt>Status</dt><dd>{{.Status}}</dd>
</dl>
{{end}}
<p>Signature on delivery: {{yesno .Delivery.Signature}}</p>
<p>Leave with a neighbour: {{yesno .Delivery.Neighbour}}</p>
{{with .Delivery.Note}}<p>Delivery note: <span id="note">{{.}}</span></p>
<p class="screen-only"><button type="button" id="summarize-note" hidden>Summarize the note</button> <span id="note-summary" role="status"></span></p>{{end}}
{{with .Delivery.Invoice}}<p>Customs invoice: {{.}}</p>{{end}}
{{with .Delivery.Photo}}<p>Photo of the contents: {{.}}</p>{{end}}
{{with .Delivery.Customs}}<p>Customs description: {{.}}</p>{{end}}
<iframe title="Label preview" src="/portal/parcels/{{.Parcel.Reference}}/label" width="420" height="140"></iframe>
<section class="screen-only" id="writing" data-reference="{{.Parcel.Reference}}" data-recipient="{{.Parcel.Recipient.Name}}" data-sender="{{.Parcel.Sender}}">
<h2>Writing to the recipient</h2>
<p><label for="brief">What it should say</label> <input id="brief" size="50" placeholder="It arrives a day late"></p>
<p><button type="button" id="draft" hidden>Draft the message</button> <button type="button" id="shorten" hidden>Make it shorter</button> <span id="drafting" role="status"></span></p>
<p><label for="message">Message to the recipient</label><br><textarea id="message" rows="5" cols="60"></textarea></p>
</section>
<script type="module" src="/portal/js/writing.js"></script>
</section>
<section id="history" role="tabpanel" aria-labelledby="history-tab" hidden>
<table>
  <thead><tr><th>When</th><th>What</th></tr></thead>
  <tbody>
    <tr><td><time datetime="{{.Parcel.CreatedAt.UTC.Format "2006-01-02T15:04:05Z"}}"></time></td><td>Registered</td></tr>
    {{if ne .Parcel.Status "REGISTERED"}}<tr><td><time datetime="{{.Parcel.UpdatedAt.UTC.Format "2006-01-02T15:04:05Z"}}"></time></td><td>{{.Parcel.Status}}</td></tr>{{end}}
  </tbody>
</table>
</section>
<p class="screen-only"><a href="/portal/parcels?shop={{.Parcel.Sender}}">Your parcels</a></p>
<script>
  const $ = (id) => document.getElementById(id);
  for (const tab of ['details', 'history']) {
    $(tab + '-tab').addEventListener('click', () => {
      for (const other of ['details', 'history']) {
        $(other + '-tab').setAttribute('aria-selected', String(other === tab));
        $(other).hidden = other !== tab;
      }
    });
  }
  $('actions').addEventListener('click', () => {
    $('actions-menu').hidden = !$('actions-menu').hidden;
    $('actions').setAttribute('aria-expanded', String(!$('actions-menu').hidden));
  });
  $('cancel')?.addEventListener('click', () => {
    $('actions-menu').hidden = true;
    if (confirm('Cancel parcel {{.Parcel.Reference}}? It will not be collected.')) $('cancel-form').submit();
  });
  $('report').addEventListener('click', () => {
    $('actions-menu').hidden = true;
    const note = prompt('What went wrong with parcel {{.Parcel.Reference}}?');
    if (note) {
      $('report-form').elements.note.value = note;
      $('report-form').submit();
    }
  });
</script>
{{end}}{{template "layout" .}}`)
	page("label", `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><link rel="icon" href="/portal/icon.svg" type="image/svg+xml"><title>Label {{.Reference}}</title></head>
<body style="font: 14px monospace; margin: 8px">
  <p>Ship to: {{.Recipient.Name}}, {{.Recipient.Street}}, {{.Recipient.Postcode}} {{.Recipient.City}}</p>
  <p>{{.ServiceLevel}} · Barcode: {{.Barcode}}</p>
</body>
</html>`)
	page("track", `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="icon" href="/portal/icon.svg" type="image/svg+xml"><title>Parcel {{.Reference}}</title>
<meta name="description" content="Follow parcel {{.Reference}}: when it arrives, and each depot it passes."></head>
<body style="font: 16px/1.5 sans-serif; margin: 24px" data-reference="{{.Reference}}">
  <main>
  <h1>Parcel {{.Reference}}</h1>
  <p>Your parcel from {{.Sender}} is {{.Status}}.</p>
  <p id="estimate" role="status">Working out when your parcel arrives</p>
  <p id="scan" role="status"></p>
  {{if .Instructions}}<section>
  <h2>Delivery instructions</h2>
  <p>Tell your courier where to leave the parcel, in any language: they read it in English.</p>
  <p><label for="instructions">Instructions for your courier</label><br><textarea id="instructions" rows="3" cols="60">{{.Note}}</textarea></p>
  <p><button id="dictate" type="button">Dictate</button> <button id="check-spelling" type="button">Check spelling</button> <button id="save-instructions" type="button">Save instructions</button></p>
  <p id="spelling" role="status"></p>
  <p><button id="use-correction" type="button" hidden>Use the correction</button></p>
  <p id="instructions-status" role="status"></p>
  </section>{{end}}
  {{if .Returns}}<section>
  <h2>Send it back</h2>
  <p>Tell us why, in your own words: we suggest the reason, and you can choose another.</p>
  <p><label for="return-why">Why are you sending it back?</label><br><textarea id="return-why" rows="3" cols="60"></textarea></p>
  <p><label for="return-reason">Reason</label> <select id="return-reason">
    <option value="">Choose a reason</option>
    <option value="DAMAGED">Damaged</option>
    <option value="WRONG_ITEM">Wrong item</option>
    <option value="NOT_AS_DESCRIBED">Not as described</option>
    <option value="NOT_NEEDED">No longer needed</option>
  </select></p>
  <p><button id="send-back" type="button">Send it back</button></p>
  <p id="return-status" role="status"></p>
  </section>{{end}}
  {{if .Damage}}<section>
  <h2>Arrived damaged?</h2>
  <p>Send us a photo of the parcel, and the shop that sent it files the claim.</p>
  <p><label for="damage-photo">Photo of the parcel</label> <input id="damage-photo" type="file" accept="image/png, image/jpeg"></p>
  <p id="damage-status" role="status"></p>
  </section>{{end}}
  </main>
  <script src="/portal/js/tracking.js"></script>
  {{if .Instructions}}<script type="module" src="/portal/js/instructions.js"></script>{{end}}
  {{if .Damage}}<script type="module" src="/portal/js/damage.js"></script>{{end}}
  {{if .Returns}}<script type="module" src="/portal/js/returns.js"></script>{{end}}
</body>
</html>`)
	page("parcels", `{{define "body"}}
<p data-testid="parcel-count">{{len .Parcels}} parcels</p>
<p><label for="find">Find a parcel</label> <input id="find" placeholder="PX-..."></p>
<p><a href="/portal/parcels.csv?shop={{.Shop}}" download>Export as CSV</a> <a href="/portal/settings?shop={{.Shop}}">Settings</a></p>
<table>
  <thead><tr><th>Parcel</th><th>Recipient</th><th>Service</th><th>Status</th></tr></thead>
  <tbody>
  {{range $i, $p := .Parcels}}<tr data-testid="parcel-row" data-reference="{{.Reference}}"{{if ge $i 20}} hidden{{end}}><td><a href="/portal/parcels/{{.Reference}}">{{.Reference}}</a></td><td>{{.Recipient.Name}}</td><td>{{.ServiceLevel}}</td><td>{{.Status}}</td></tr>{{end}}
  </tbody>
</table>
<div id="more"></div>
<button id="top" type="button" hidden>Back to the top</button>
<ul id="row-menu" role="menu" hidden>
  <li><a role="menuitem" id="row-label" download>Print the label</a></li>
  <li><a role="menuitem" id="row-track" target="_blank">Track this parcel</a></li>
</ul>
{{if .Pickups}}
<h2>Pickups</h2>
<p>Drag a parcel onto the day it is picked up.</p>
<p>{{range .Pickups}}<span class="chip" draggable="true" aria-label="Pickup {{.Reference}}" data-reference="{{.Reference}}" data-summary="{{.Reference}}: {{kilos .WeightGrams}} to {{.Recipient.Name}}">{{.Reference}}</span>{{end}}</p>
<div class="slots">
  <section class="slot" data-day="Tomorrow"><h3>Tomorrow</h3></section>
  <section class="slot" data-day="Friday"><h3>Friday</h3></section>
</div>
<p id="planned" role="status"></p>
{{end}}
<script>
  const $ = (id) => document.getElementById(id);
  // Enter opens the parcel with that reference.
  $('find').addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && e.target.value.trim()) location.href = '/portal/parcels/' + encodeURIComponent(e.target.value.trim());
  });
  // A double-click opens a parcel; a right-click opens its menu.
  for (const row of document.querySelectorAll('[data-testid=parcel-row]')) {
    const ref = row.dataset.reference;
    row.addEventListener('dblclick', () => { location.href = '/portal/parcels/' + ref; });
    row.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      $('row-label').href = '/portal/parcels/' + ref + '/label.zpl';
      $('row-track').href = '/portal/track/' + ref;
      $('row-menu').hidden = false;
    });
  }
  // Down a long list, a button goes back to the top.
  addEventListener('scroll', () => { $('top').hidden = scrollY < 400; });
  $('top').addEventListener('click', () => scrollTo(0, 0));
  // The list shows 20 parcels, and 20 more as it scrolls.
  new IntersectionObserver((es) => {
    if (!es[0].isIntersecting) return;
    [...document.querySelectorAll('[data-testid=parcel-row][hidden]')].slice(0, 20).forEach((r) => { r.hidden = false; });
  }).observe($('more'));
  // Pickups are planned by dragging a parcel onto a day.
  let dragged;
  for (const chip of document.querySelectorAll('.chip')) {
    chip.addEventListener('dragstart', () => { dragged = chip; });
    chip.addEventListener('click', () => { $('planned').textContent = chip.dataset.summary; });
  }
  for (const slot of document.querySelectorAll('.slot')) {
    slot.addEventListener('dragover', (e) => e.preventDefault());
    slot.addEventListener('drop', async (e) => {
      e.preventDefault();
      const body = new URLSearchParams({ reference: dragged.dataset.reference, day: slot.dataset.day });
      let r;
      try {
        r = await fetch('/portal/pickups', { method: 'POST', body });
      } catch {
        $('planned').textContent = 'The pickup could not be planned: check your connection and try again';
        return;
      }
      if (!r.ok) {
        $('planned').textContent = 'Pickups cannot be planned right now: try again later';
        return;
      }
      slot.appendChild(dragged);
      $('planned').textContent = dragged.dataset.reference + ' is picked up ' + (slot.dataset.day === 'Tomorrow' ? 'tomorrow' : 'on ' + slot.dataset.day);
    });
  }
</script>
{{end}}{{template "layout" .}}`)
	page("settings", `{{define "body"}}
<form method="post" action="/portal/settings" enctype="multipart/form-data">
  <input type="hidden" name="shop" value="{{.Settings.Shop}}">
  <p><label for="account">Shop account</label> <input id="account" value="{{.Settings.Shop}}" disabled></p>
  <p><label for="address">Pickup address</label> <input id="address" name="address" value="{{.Settings.PickupAddress}}"></p>
  <p>
    <label for="days">Pickup days</label>
    <select id="days" name="days" multiple size="5">
      {{range weekdays}}<option{{if has $.Settings.PickupDays .}} selected{{end}}>{{.}}</option>{{end}}
    </select>
  </p>
  <p><label><input type="checkbox" name="notify"{{if .Settings.NotifyDelivered}} checked{{end}}> Email me when a parcel is delivered</label></p>
  <p>
    <button type="button" id="logo-button">Upload your logo</button>
    <input type="file" id="logo" name="logo" accept="image/*" hidden>
    <span id="logo-name">{{with .Settings.Logo}}Logo: {{.}}{{end}}</span>
  </p>
  <button type="submit">Save the settings</button>
</form>
<script>
  document.getElementById('logo-button').addEventListener('click', () => document.getElementById('logo').click());
  document.getElementById('logo').addEventListener('change', (e) => {
    document.getElementById('logo-name').textContent = 'Logo: ' + e.target.files[0].name;
  });
</script>
{{end}}{{template "layout" .}}`)
}

// portalForm holds what a shop typed, to show it again with an error.
type portalForm struct {
	Sender, Reference, Weight, Service, Name, Street, Postcode, City, Country string
	Contents, Customs, Note                                                   string
	Signature, Neighbour                                                      bool
	Ready                                                                     bool
}

type portalPage struct {
	Title, Error string
	Notice       string
	Shop         string
	Pickups      []*Parcel // the shop's parcels still to be picked up
	Settings     *shopSettings
	Form         portalForm
	// Customs says the registration form drafts customs descriptions, and
	// cuts the contents' photo out of its background: the portal has the
	// models its page runs for them.
	Customs  bool
	Quote    *quote
	Parcel   *Parcel
	Parcels  []*Parcel
	Delivery struct {
		Signature, Neighbour          bool
		Invoice, Photo, Customs, Note string
	}
}

// portalScripts are the scripts of the pages that have their own.
//
//go:embed web/js
var portalScripts embed.FS

// portalIcon is the portal's icon: a parcel.
const portalIcon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">
  <rect x="3" y="7" width="26" height="20" rx="2" fill="#c8894b"/>
  <rect x="3" y="7" width="26" height="6" fill="#a86d34"/>
  <rect x="13" y="7" width="6" height="10" fill="#f2e3c9"/>
</svg>
`

// drafts reports whether the portal drafts customs descriptions: it has the
// model its page runs, and what runs it.
func (s *service) drafts() bool { return s.portalModels != "" && s.portalVendor != "" }

// returnSuggestions says the tracking page suggests why a recipient sends a
// parcel back: the portal serves the return reasons' model (WebLLM's, which
// fetches a model's files under resolve/main/, as Hugging Face serves
// them).
func (s *service) returnSuggestions() bool {
	if !s.drafts() {
		return false
	}
	_, err := os.Stat(filepath.Join(s.portalModels, "return-reason", "resolve", "main", "mlc-chat-config.json"))
	return err == nil
}

// photoChecks says the tracking page checks recipients' photos of damaged
// parcels: the portal serves the photo check's model.
func (s *service) photoChecks() bool {
	if !s.drafts() {
		return false
	}
	_, err := os.Stat(filepath.Join(s.portalModels, "photo-check", "model.json"))
	return err == nil
}

func (s *service) portalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /portal/icon.svg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "max-age=86400")
		_, _ = io.WriteString(w, portalIcon)
	})
	scripts, err := fs.Sub(portalScripts, "web/js")
	if err != nil {
		panic(err)
	}
	mux.Handle("GET /portal/js/", http.StripPrefix("/portal/js/", http.FileServerFS(scripts)))
	if s.drafts() {
		// The models the portal's pages run themselves, and what runs them.
		mux.Handle("GET /portal/models/", http.StripPrefix("/portal/models/", http.FileServer(http.Dir(s.portalModels))))
		mux.Handle("GET /portal/vendor/", http.StripPrefix("/portal/vendor/", http.FileServer(http.Dir(s.portalVendor))))
	}
	mux.HandleFunc("GET /portal/{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/portal/quote", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /portal/quote", func(w http.ResponseWriter, _ *http.Request) {
		s.render(w, "quote", http.StatusOK, portalPage{Title: "Get a quote"})
	})
	mux.HandleFunc("POST /portal/quote", s.portalQuote)
	mux.HandleFunc("GET /portal/parcels/new", func(w http.ResponseWriter, _ *http.Request) {
		s.render(w, "register", http.StatusOK, portalPage{Title: "Register a parcel", Form: portalForm{Neighbour: true}, Customs: s.drafts()})
	})
	mux.HandleFunc("POST /portal/parcels", s.portalRegister)
	mux.HandleFunc("GET /portal/parcels", s.portalParcels)
	mux.HandleFunc("GET /portal/parcels/{reference}", s.portalParcel)
	mux.HandleFunc("GET /portal/parcels/{reference}/label", s.portalLabel)
	mux.HandleFunc("GET /portal/parcels/{reference}/label.zpl", s.portalLabelFile)
	mux.HandleFunc("POST /portal/parcels/{reference}/cancel", s.portalCancel)
	mux.HandleFunc("POST /portal/parcels/{reference}/problems", s.portalProblem)
	mux.HandleFunc("GET /portal/parcels.csv", s.portalExport)
	mux.HandleFunc("GET /portal/track/{reference}", s.portalTrack)
	mux.HandleFunc("GET /portal/track/{reference}/estimate", s.portalEstimate)
	mux.HandleFunc("GET /portal/track/{reference}/live", s.portalLive)
	mux.HandleFunc("POST /portal/track/{reference}/photos", s.portalDamagePhoto)
	mux.HandleFunc("POST /portal/track/{reference}/returns", s.portalReturn)
	mux.HandleFunc("POST /portal/track/{reference}/note", s.portalNote)
	mux.HandleFunc("POST /portal/pickups", s.portalPickup)
	mux.HandleFunc("GET /portal/settings", s.portalSettings)
	mux.HandleFunc("POST /portal/settings", s.portalSaveSettings)
}

func (s *service) render(w http.ResponseWriter, page string, status int, data portalPage) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := portalPages[page].ExecuteTemplate(w, page, data); err != nil {
		s.log.Error("rendering a portal page failed", "page", page, "err", err)
	}
}

func (s *service) portalQuote(w http.ResponseWriter, r *http.Request) {
	f := portalForm{Weight: r.FormValue("weight"), Country: r.FormValue("country"), Postcode: strings.TrimSpace(r.FormValue("postcode")), Service: r.FormValue("service")}
	page := portalPage{Title: "Get a quote", Form: f}
	weight, msg := portalWeight(f.Weight)
	switch {
	case msg != "":
		page.Error = msg
	case f.Country == "":
		page.Error = "Choose the destination country"
	case f.Postcode == "":
		page.Error = "Enter the postcode"
	}
	if page.Error != "" {
		s.render(w, "quote", http.StatusUnprocessableEntity, page)
		return
	}
	zone, err := s.zone(r.Context(), f.Country, f.Postcode)
	if msg := addressMessage(err); msg != "" {
		page.Error = msg
		s.render(w, "quote", http.StatusUnprocessableEntity, page)
		return
	}
	q, err := s.priceFor(r.Context(), zone, f.Country, level(f.Service), weight)
	if msg, _ := ratingMessage(err); msg != "" {
		page.Error = msg
		s.render(w, "quote", http.StatusUnprocessableEntity, page)
		return
	}
	page.Quote = &q
	s.render(w, "quote", http.StatusOK, page)
}

func (s *service) portalRegister(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(4 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		s.render(w, "register", http.StatusBadRequest, portalPage{Title: "Register a parcel", Error: "The form could not be read"})
		return
	}
	f := portalForm{
		Sender: strings.TrimSpace(r.FormValue("sender")), Reference: strings.TrimSpace(r.FormValue("reference")),
		Weight: r.FormValue("weight"), Service: r.FormValue("service"),
		Name: strings.TrimSpace(r.FormValue("name")), Street: strings.TrimSpace(r.FormValue("street")),
		Postcode: strings.TrimSpace(r.FormValue("postcode")), City: strings.TrimSpace(r.FormValue("city")), Country: r.FormValue("country"),
		Signature: r.FormValue("signature") != "", Neighbour: r.FormValue("neighbour") != "",
		Contents: strings.TrimSpace(r.FormValue("contents")), Customs: strings.TrimSpace(r.FormValue("customs")),
		Note: strings.TrimSpace(r.FormValue("note")),
	}
	f.Ready = f.Sender != "" && f.Reference != ""
	page := portalPage{Title: "Register a parcel", Form: f, Customs: s.drafts()}
	weight, msg := portalWeight(f.Weight)
	switch {
	case !referencePattern.MatchString(f.Reference):
		page.Error = "A parcel reference has 3 to 40 capital letters, digits and dashes"
	case msg != "":
		page.Error = msg
	case f.Name == "" || f.Postcode == "" || f.Country == "":
		page.Error = "Enter the recipient's name, postcode and country"
	}
	if page.Error != "" {
		s.render(w, "register", http.StatusUnprocessableEntity, page)
		return
	}
	extra := map[string]any{"signature": f.Signature, "leaveWithNeighbour": f.Neighbour}
	if file, h, err := r.FormFile("invoice"); err == nil {
		_ = file.Close()
		extra["customsInvoice"] = h.Filename
	}
	// The contents' photo, cut out of its background in the page (js/cutout.js).
	if file, h, err := r.FormFile("contents-photo"); err == nil {
		_ = file.Close()
		extra["contentsPhoto"] = h.Filename
	}
	if f.Contents != "" {
		extra["contents"] = f.Contents
	}
	if f.Customs != "" {
		extra["customsDescription"] = f.Customs
	}
	if f.Note != "" {
		extra["deliveryNote"] = f.Note
	}
	_, err := s.register(r.Context(), registration{
		Reference: f.Reference, Sender: f.Sender, WeightGrams: weight, ServiceLevel: level(f.Service), Source: "portal",
		Recipient: Recipient{Name: f.Name, Street: f.Street, City: f.City, Postcode: f.Postcode, Country: f.Country},
		Extra:     extra,
	})
	switch {
	case errors.Is(err, errDuplicate):
		s.refused(f.Reference, "already registered")
		page.Error = fmt.Sprintf("Parcel %s is already registered", f.Reference)
	case errors.Is(err, errUnavailable):
		s.refused(f.Reference, "the database keeps failing")
		page.Error = "The parcel could not be stored; try again later"
	case addressMessage(err) != "":
		s.refused(f.Reference, err.Error())
		page.Error = addressMessage(err)
	case err != nil:
		s.log.Error("portal registration failed", "reference", f.Reference, "err", err)
		page.Error = "Something went wrong; try again later"
	default:
		http.Redirect(w, r, "/portal/parcels/"+f.Reference, http.StatusSeeOther)
		return
	}
	s.render(w, "register", http.StatusUnprocessableEntity, page)
}

func (s *service) portalParcel(w http.ResponseWriter, r *http.Request) {
	p, ok := s.portalLookup(w, r)
	if !ok {
		return
	}
	s.showParcel(w, http.StatusOK, p, portalPage{})
}

// portalLookup finds the parcel of the request's path, or answers that it
// cannot.
func (s *service) portalLookup(w http.ResponseWriter, r *http.Request) (*Parcel, bool) {
	ref := r.PathValue("reference")
	p, err := s.store.Get(r.Context(), ref)
	if errors.Is(err, errNotFound) {
		s.render(w, "parcels", http.StatusNotFound, portalPage{Title: "No parcel " + ref})
		return nil, false
	}
	if err != nil {
		s.log.Error("portal lookup failed", "reference", ref, "err", err)
		s.render(w, "parcels", http.StatusServiceUnavailable, portalPage{Title: "Parcels", Error: "Try again later"})
		return nil, false
	}
	return p, true
}

// showParcel shows a parcel's page, with the notice or error of page.
func (s *service) showParcel(w http.ResponseWriter, status int, p *Parcel, page portalPage) {
	page.Title, page.Parcel = "Parcel "+p.Reference+" registered", p
	var d struct {
		Signature bool   `json:"signature"`
		Neighbour bool   `json:"leaveWithNeighbour"`
		Invoice   string `json:"customsInvoice"`
		Photo     string `json:"contentsPhoto"`
		Customs   string `json:"customsDescription"`
		Note      string `json:"deliveryNote"`
	}
	_ = json.Unmarshal(p.Details, &d)
	page.Delivery.Signature, page.Delivery.Neighbour, page.Delivery.Invoice, page.Delivery.Customs = d.Signature, d.Neighbour, d.Invoice, d.Customs
	page.Delivery.Photo = d.Photo
	page.Delivery.Note = d.Note
	s.render(w, "parcel", status, page)
}

// portalLabel previews a parcel's shipping label.
func (s *service) portalLabel(w http.ResponseWriter, r *http.Request) {
	p, ok := s.portalLookup(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	l := s.labels.label(p)
	_ = portalPages["label"].Execute(w, struct {
		*Parcel
		Barcode string
	}{p, l.Barcode})
}

// portalLabelFile is a parcel's shipping label for the shop's label printer (ZPL).
func (s *service) portalLabelFile(w http.ResponseWriter, r *http.Request) {
	p, ok := s.portalLookup(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/zpl")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", p.Reference+"-label.zpl"))
	_, _ = w.Write([]byte(zpl(p, s.labels.label(p))))
}

// zpl lays out a shipping label in ZPL: the recipient, the barcode, and the
// signature depot scanners check.
func zpl(p *Parcel, l shippingLabel) string {
	var b strings.Builder
	b.WriteString("^XA\n^CI28\n")
	fmt.Fprintf(&b, "^FO40,40^A0N,36,36^FD%s^FS\n", l.ServiceLevel)
	fmt.Fprintf(&b, "^FO40,100^A0N,32,32^FD%s^FS\n", p.Recipient.Name)
	fmt.Fprintf(&b, "^FO40,140^A0N,32,32^FD%s^FS\n", p.Recipient.Street)
	fmt.Fprintf(&b, "^FO40,180^A0N,32,32^FD%s %s %s^FS\n", p.Recipient.Postcode, p.Recipient.City, p.Recipient.Country)
	fmt.Fprintf(&b, "^FO40,260^BY3^BCN,120,Y,N,N^FD%s^FS\n", l.Barcode)
	fmt.Fprintf(&b, "^FO40,440^A0N,24,24^FDRef %s^FS\n", l.Reference)
	fmt.Fprintf(&b, "^FO40,480^A0N,16,16^FD%s^FS\n", l.Signature)
	b.WriteString("^XZ\n")
	return b.String()
}

// portalCancel cancels a parcel that has not been picked up yet.
func (s *service) portalCancel(w http.ResponseWriter, r *http.Request) {
	p, ok := s.portalLookup(w, r)
	if !ok {
		return
	}
	err := s.store.Cancel(r.Context(), p.Reference, changeable)
	switch {
	case errors.Is(err, errNotChangeable):
		s.showParcel(w, http.StatusConflict, p, portalPage{Error: fmt.Sprintf("Parcel %s is %s: it can no longer be cancelled", p.Reference, p.Status)})
		return
	case err != nil:
		s.log.Error("portal cancellation failed", "reference", p.Reference, "err", err)
		s.render(w, "parcels", http.StatusServiceUnavailable, portalPage{Title: "Your parcels", Error: "Try again later"})
		return
	}
	s.log.Info("parcel cancelled", "reference", p.Reference, "source", "portal")
	s.callOffCollection(r.Context(), p)
	http.Redirect(w, r, "/portal/parcels?shop="+p.Sender+"&cancelled="+p.Reference, http.StatusSeeOther)
}

// portalProblem takes a shop's report of a problem with a parcel, for the
// support team.
func (s *service) portalProblem(w http.ResponseWriter, r *http.Request) {
	p, ok := s.portalLookup(w, r)
	if !ok {
		return
	}
	note := strings.TrimSpace(r.FormValue("note"))
	s.log.Info("problem reported", "reference", p.Reference, "sender", p.Sender, "note", note)
	s.showParcel(w, http.StatusOK, p, portalPage{Notice: fmt.Sprintf("Problem reported: %s. Our support team will get back to you.", note)})
}

// portalExport is a shop's parcels as CSV.
func (s *service) portalExport(w http.ResponseWriter, r *http.Request) {
	shop := r.URL.Query().Get("shop")
	ps, err := s.store.List(r.Context(), shop)
	if err != nil {
		s.log.Error("portal export failed", "shop", shop, "err", err)
		http.Error(w, "try again later", http.StatusServiceUnavailable)
		return
	}
	var b bytes.Buffer
	c := csv.NewWriter(&b)
	_ = c.Write([]string{"Parcel", "Recipient", "Service", "Status"})
	for _, p := range ps {
		_ = c.Write([]string{p.Reference, p.Recipient.Name, p.ServiceLevel, p.Status})
	}
	c.Flush()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "parcels-"+shop+".csv"))
	_, _ = w.Write(b.Bytes())
}

// portalTrack is the page shops share with recipients.
func (s *service) portalTrack(w http.ResponseWriter, r *http.Request) {
	p, ok := s.portalLookup(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var d struct {
		Note string `json:"deliveryNote"`
	}
	_ = json.Unmarshal(p.Details, &d)
	_ = portalPages["track"].Execute(w, trackPage{
		Reference: p.Reference, Sender: p.Sender, Status: strings.ToLower(strings.ReplaceAll(p.Status, "_", " ")),
		Damage: p.Status == "DELIVERED" && s.photoChecks(), Instructions: p.Status != "DELIVERED", Note: d.Note,
		Returns: p.Status == "DELIVERED" && s.returnSuggestions(),
	})
}

// trackPage is what the tracking page shows: Damage, that a recipient can
// send a photo of the parcel, delivered damaged; Instructions, that they can
// tell the courier where to leave it (Note, what they told them); Returns,
// that they can send it back, the page suggesting why from their words.
type trackPage struct {
	Reference, Sender, Status string
	Damage, Instructions      bool
	Returns                   bool
	Note                      string
}

// portalNote keeps the delivery instructions a recipient gives on the
// tracking page, in English (js/instructions.js translates them), as the
// parcel's delivery note, which the couriers' apps show.
func (s *service) portalNote(w http.ResponseWriter, r *http.Request) {
	p, ok := s.portalLookup(w, r)
	if !ok {
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil || strings.TrimSpace(body.Note) == "" || len([]rune(body.Note)) > maxDeliveryNote {
		http.Error(w, fmt.Sprintf("a note of at most %d characters", maxDeliveryNote), http.StatusUnprocessableEntity)
		return
	}
	if p.Status == "DELIVERED" {
		http.Error(w, "the parcel was delivered", http.StatusConflict)
		return
	}
	if err := s.store.SetDeliveryNote(r.Context(), p.Reference, strings.TrimSpace(body.Note)); err != nil {
		s.log.Error("delivery note not kept", "reference", p.Reference, "err", err)
		http.Error(w, "try again later", http.StatusServiceUnavailable)
		return
	}
	s.log.Info("delivery note kept", "reference", p.Reference)
	w.WriteHeader(http.StatusNoContent)
}

// maxDamagePhoto is the largest photo of a damaged parcel the tracking page
// sends.
const maxDamagePhoto = 10 << 20

// portalDamagePhoto keeps the photo a recipient sends of a delivered parcel
// that arrived damaged: the tracking page sends one its photo check says
// shows a parcel (js/damage.js).
func (s *service) portalDamagePhoto(w http.ResponseWriter, r *http.Request) {
	p, ok := s.portalLookup(w, r)
	if !ok {
		return
	}
	contentType := r.Header.Get("Content-Type")
	if p.Status != "DELIVERED" || (contentType != "image/png" && contentType != "image/jpeg") {
		http.Error(w, "a photo (PNG or JPEG) of a delivered parcel", http.StatusUnprocessableEntity)
		return
	}
	photo, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxDamagePhoto))
	if err != nil || len(photo) == 0 {
		http.Error(w, "a photo of at most 10 MB", http.StatusRequestEntityTooLarge)
		return
	}
	if err := s.store.AddDamagePhoto(r.Context(), p.Reference, contentType, photo); err != nil {
		s.log.Error("damage photo not kept", "reference", p.Reference, "err", err)
		http.Error(w, "try again later", http.StatusServiceUnavailable)
		return
	}
	s.log.Info("damage photo received", "reference", p.Reference, "bytes", len(photo))
	w.WriteHeader(http.StatusCreated)
}

// returnReasons are the reasons a recipient sends a parcel back for.
var returnReasons = map[string]bool{"DAMAGED": true, "WRONG_ITEM": true, "NOT_AS_DESCRIBED": true, "NOT_NEEDED": true}

// portalReturn keeps a recipient's request to send a delivered parcel back.
func (s *service) portalReturn(w http.ResponseWriter, r *http.Request) {
	p, ok := s.portalLookup(w, r)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
		Why    string `json:"why"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil || !returnReasons[req.Reason] || p.Status != "DELIVERED" {
		http.Error(w, "a delivered parcel's reason to send it back", http.StatusUnprocessableEntity)
		return
	}
	if err := s.store.AddReturn(r.Context(), p.Reference, req.Reason, req.Why); err != nil {
		s.log.Error("return not kept", "reference", p.Reference, "err", err)
		http.Error(w, "try again later", http.StatusServiceUnavailable)
		return
	}
	s.log.Info("return requested", "reference", p.Reference, "reason", req.Reason)
	w.WriteHeader(http.StatusCreated)
}

func (s *service) portalParcels(w http.ResponseWriter, r *http.Request) {
	shop := r.URL.Query().Get("shop")
	if c, err := r.Cookie("shop"); err == nil && shop == "" {
		shop = c.Value // a shop that came before
	}
	ps, err := s.store.List(r.Context(), shop)
	if err != nil {
		s.log.Error("portal list failed", "shop", shop, "err", err)
		s.render(w, "parcels", http.StatusServiceUnavailable, portalPage{Title: "Your parcels", Error: "Try again later"})
		return
	}
	page := portalPage{Title: "Your parcels", Parcels: ps, Shop: shop}
	for _, p := range ps {
		if shop != "" && p.Status == "REGISTERED" {
			page.Pickups = append(page.Pickups, p)
		}
	}
	if ref := r.URL.Query().Get("cancelled"); ref != "" {
		page.Notice = "Parcel " + ref + " cancelled"
	}
	s.render(w, "parcels", http.StatusOK, page)
}

func portalWeight(v string) (int, string) {
	if strings.TrimSpace(v) == "" {
		return 0, "Enter the weight in grams"
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	switch {
	case err != nil || n <= 0:
		return 0, "Enter the weight in grams"
	case n > maxWeightGrams:
		return 0, "A parcel weighs at most 30 kg"
	}
	return n, ""
}

func level(service string) string {
	if service == "EXPRESS" {
		return "EXPRESS"
	}
	return "STANDARD"
}

// addressMessage words the address check's errors for shops ("" for other
// errors and for none).
func addressMessage(err error) string {
	var und *undeliverableError
	var up *upstreamError
	switch {
	case errors.As(err, &und):
		return "We cannot deliver to that address: " + und.reason
	case errors.As(err, &up):
		return "The address check is unavailable; try again later"
	}
	return ""
}

// portalPickup plans the day a parcel is picked up.
func (s *service) portalPickup(w http.ResponseWriter, r *http.Request) {
	ref, day := r.FormValue("reference"), r.FormValue("day")
	if ref == "" || (day != "Tomorrow" && day != "Friday") {
		http.Error(w, "a pickup needs a parcel and a day", http.StatusBadRequest)
		return
	}
	if err := s.store.PlanPickup(r.Context(), ref, day); err != nil {
		s.log.Error("planning a pickup failed", "reference", ref, "err", err)
		http.Error(w, "try again later", http.StatusServiceUnavailable)
		return
	}
	s.log.Info("pickup planned", "reference", ref, "day", day)
	if err := s.courier.pickup(r.Context(), ref, day); err != nil {
		s.log.Error("telling the courier about a pickup failed", "reference", ref, "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *service) portalSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Settings(r.Context(), r.URL.Query().Get("shop"))
	if err != nil {
		s.log.Error("reading settings failed", "err", err)
		s.render(w, "parcels", http.StatusServiceUnavailable, portalPage{Title: "Settings", Error: "Try again later"})
		return
	}
	s.render(w, "settings", http.StatusOK, portalPage{Title: "Settings", Settings: st})
}

func (s *service) portalSaveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		s.render(w, "settings", http.StatusBadRequest, portalPage{Title: "Settings", Error: "The form could not be read"})
		return
	}
	st := &shopSettings{
		Shop: r.FormValue("shop"), PickupAddress: strings.TrimSpace(r.FormValue("address")),
		PickupDays: r.Form["days"], NotifyDelivered: r.FormValue("notify") != "",
	}
	if f, h, err := r.FormFile("logo"); err == nil {
		_ = f.Close()
		st.Logo = h.Filename
	}
	if err := s.store.SaveSettings(r.Context(), st); err != nil {
		s.log.Error("saving settings failed", "shop", st.Shop, "err", err)
		s.render(w, "settings", http.StatusServiceUnavailable, portalPage{Title: "Settings", Settings: st, Error: "Try again later"})
		return
	}
	saved, err := s.store.Settings(r.Context(), st.Shop)
	if err != nil {
		saved = st
	}
	http.SetCookie(w, &http.Cookie{Name: "shop", Value: st.Shop, Path: "/portal", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode})
	s.render(w, "settings", http.StatusOK, portalPage{Title: "Settings", Settings: saved, Notice: "Settings saved"})
}
