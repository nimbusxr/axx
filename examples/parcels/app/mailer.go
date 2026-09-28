package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// The service emails shops at the contact address of their settings:
//
//	Parcel <reference> registered   for each parcel they register, with its
//	                                label attached, for their label printer
//	Parcel <reference> delivered    when it is delivered, if they asked to be
//	                                told in the portal's settings
//
// It sends its mail by SMTP to its mail server (PARCELS_SMTP_ADDR).
type mailer struct {
	addr, from string
	log        *slog.Logger
}

type attachment struct {
	name, mediaType string
	content         []byte
}

// send sends an email with a text and an HTML body, and attachments.
func (m *mailer) send(ctx context.Context, to, subject, text, htmlBody string, files ...attachment) error {
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	mixed, alt := "mixed-"+hex.EncodeToString(raw[:4]), "alt-"+hex.EncodeToString(raw[4:])
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\n",
		m.from, to, mime.QEncoding.Encode("utf-8", subject), time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", mixed)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: multipart/alternative; boundary=%q\r\n\r\n", mixed, alt)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", alt, lines(text))
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", alt, lines(htmlBody))
	fmt.Fprintf(&b, "--%s--\r\n", alt)
	for _, f := range files {
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; name=%q\r\nContent-Disposition: attachment; filename=%q\r\n"+
			"Content-Transfer-Encoding: base64\r\n\r\n%s\r\n", mixed, f.mediaType, f.name, f.name, lines(string(f.content)))
	}
	fmt.Fprintf(&b, "--%s--\r\n", mixed)

	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", m.addr)
	if err != nil {
		return err
	}
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	host, _, _ := net.SplitHostPort(m.addr)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = c.Close() }()
	from := m.from
	if i := strings.LastIndex(from, "<"); i >= 0 {
		from = strings.TrimSuffix(from[i+1:], ">")
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	wc, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := wc.Write(b.Bytes()); err != nil {
		return err
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// lines is text in base64, in lines of 76 characters.
func lines(s string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(s))
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc)
	return b.String()
}

// registered emails a shop its parcel's label.
func (s *service) mailRegistered(ctx context.Context, p *Parcel) {
	st, err := s.store.Settings(ctx, p.Sender)
	if err != nil || st.ContactEmail == "" {
		return
	}
	subject := "Parcel " + p.Reference + " registered"
	text := fmt.Sprintf("Your parcel %s is registered (%s, %d g). Its label is attached, for your label printer.",
		p.Reference, p.ServiceLevel, p.WeightGrams)
	body := fmt.Sprintf("<p>Your parcel <b>%s</b> is registered (%s, %d g).</p><p>Its label is attached, for your label printer.</p>",
		html.EscapeString(p.Reference), html.EscapeString(p.ServiceLevel), p.WeightGrams)
	label := attachment{name: p.Reference + "-label.zpl", mediaType: "application/zpl", content: []byte(zpl(p, s.labels.label(p)))}
	if err := s.mail.send(ctx, st.ContactEmail, subject, text, body, label); err != nil {
		s.log.Error("emailing the shop failed", "reference", p.Reference, "shop", p.Sender, "err", err)
	}
}

// mailDelivered tells a shop that asked to be told that its parcel was
// delivered.
func (r *recorder) mailDelivered(ctx context.Context, p *Parcel, s scan) {
	st, err := r.store.Settings(ctx, p.Sender)
	if err != nil || !st.NotifyDelivered || st.ContactEmail == "" {
		return
	}
	subject := "Parcel " + p.Reference + " delivered"
	text := fmt.Sprintf("Your parcel %s was delivered in %s at %s.", p.Reference, s.Location, s.ScannedAt.Format("15:04 on January 2"))
	body := fmt.Sprintf("<p>Your parcel <b>%s</b> was delivered in %s at %s.</p>", html.EscapeString(p.Reference),
		html.EscapeString(s.Location), s.ScannedAt.Format("15:04 on January 2"))
	if err := r.mail.send(ctx, st.ContactEmail, subject, text, body); err != nil {
		r.log.Error("emailing the shop failed", "reference", p.Reference, "shop", p.Sender, "err", err)
	}
}
