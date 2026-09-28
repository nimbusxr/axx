package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const adminUsage = `usage: parcels admin <verb>
  label <reference>          print a parcel's shipping label (ZPL)
  cancel <reference>...      cancel parcels; with -, the references are the input's lines
  parcels --shop <sender>    print a shop's parcels as JSON`

// adminVerb is what `parcels admin` was asked to do, ready to run.
type adminVerb func(ctx context.Context, svc *service, out, errOut io.Writer) int

// admin is the operations desk's command, `parcels admin <verb>`, run where
// the service runs: it reads the service's own configuration. Cancelling
// follows the rules the API and the portal follow, and calls off the
// courier's collection of an express parcel.
//
// Exit codes: 0 done, 1 failed, 2 wrong use, 3 a parcel that can no longer
// be changed, 4 no such parcel.
func admin(args []string, in io.Reader, out, errOut io.Writer) int {
	verb, err := parseAdmin(args, in)
	if err != nil {
		fmt.Fprintf(errOut, "parcels admin: %v\n%s\n", err, adminUsage)
		return 2
	}
	cfg, err := loadConfig()
	if err != nil {
		fmt.Fprintln(errOut, "parcels admin:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	store, err := openStore(ctx, cfg.DBURL, log)
	if err != nil {
		fmt.Fprintln(errOut, "parcels admin:", err)
		return 1
	}
	defer store.Close()
	return verb(ctx, &service{
		store:   store,
		courier: &courierClient{base: cfg.CourierURL, http: &http.Client{Timeout: 5 * time.Second}},
		labels:  labeler{secret: []byte(cfg.LabelSecret)},
		log:     log,
	}, out, errOut)
}

func parseAdmin(args []string, in io.Reader) (adminVerb, error) {
	if len(args) == 0 {
		return nil, errors.New("say what to do")
	}
	switch args[0] {
	case "label":
		if len(args) != 2 {
			return nil, errors.New("label takes one parcel reference")
		}
		return func(ctx context.Context, svc *service, out, errOut io.Writer) int {
			return adminLabel(ctx, svc, args[1], out, errOut)
		}, nil
	case "cancel":
		refs := args[1:]
		if len(refs) == 1 && refs[0] == "-" {
			refs = nil
			sc := bufio.NewScanner(in)
			for sc.Scan() {
				if ref := strings.TrimSpace(sc.Text()); ref != "" {
					refs = append(refs, ref)
				}
			}
			if err := sc.Err(); err != nil {
				return nil, fmt.Errorf("reading the references: %w", err)
			}
		}
		if len(refs) == 0 {
			return nil, errors.New("cancel takes parcel references")
		}
		return func(ctx context.Context, svc *service, out, errOut io.Writer) int {
			return adminCancel(ctx, svc, refs, out, errOut)
		}, nil
	case "parcels":
		fs := flag.NewFlagSet("parcels", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		shop := fs.String("shop", "", "")
		if err := fs.Parse(args[1:]); err != nil || *shop == "" || fs.NArg() > 0 {
			return nil, errors.New("parcels takes --shop <sender>")
		}
		return func(ctx context.Context, svc *service, out, errOut io.Writer) int {
			return adminParcels(ctx, svc, *shop, out, errOut)
		}, nil
	}
	return nil, fmt.Errorf("unknown verb %q", args[0])
}

// adminLabel prints a parcel's shipping label, as the portal serves it to
// the shop's label printer.
func adminLabel(ctx context.Context, svc *service, ref string, out, errOut io.Writer) int {
	p, err := svc.store.Get(ctx, ref)
	if errors.Is(err, errNotFound) {
		fmt.Fprintf(errOut, "no parcel %s\n", ref)
		return 4
	}
	if err != nil {
		fmt.Fprintln(errOut, "parcels admin:", err)
		return 1
	}
	fmt.Fprint(out, zpl(p, svc.labels.label(p)))
	return 0
}

// adminCancel cancels parcels and says what became of each. A parcel it
// cannot cancel does not stop the others: the exit code says the worst that
// happened.
func adminCancel(ctx context.Context, svc *service, refs []string, out, errOut io.Writer) int {
	code := 0
	for _, ref := range refs {
		var cancelled *Parcel
		err := svc.store.Cancel(ctx, ref, func(p *Parcel) error {
			cancelled = p
			return changeable(p)
		})
		switch {
		case errors.Is(err, errNotFound):
			fmt.Fprintf(errOut, "no parcel %s\n", ref)
			code = max(code, 4)
		case errors.Is(err, errNotChangeable):
			fmt.Fprintf(errOut, "%s is %s: it can no longer be cancelled\n", ref, cancelled.Status)
			code = max(code, 3)
		case errors.Is(err, errLocked):
			fmt.Fprintf(errOut, "%s is being dispatched: try again in a moment\n", ref)
			code = max(code, 1)
		case err != nil:
			fmt.Fprintf(errOut, "cancelling %s failed: %v\n", ref, err)
			code = max(code, 1)
		default:
			svc.callOffCollection(ctx, cancelled)
			fmt.Fprintf(out, "cancelled %s\n", ref)
		}
	}
	return code
}

// adminParcels prints a shop's parcels, oldest first, as JSON.
func adminParcels(ctx context.Context, svc *service, shop string, out, errOut io.Writer) int {
	ps, err := svc.store.List(ctx, shop)
	if err != nil {
		fmt.Fprintln(errOut, "parcels admin:", err)
		return 1
	}
	type row struct {
		Reference    string `json:"reference"`
		Status       string `json:"status"`
		ServiceLevel string `json:"serviceLevel"`
		WeightGrams  int    `json:"weightGrams"`
	}
	list := struct {
		Shop    string `json:"shop"`
		Parcels []row  `json:"parcels"`
	}{Shop: shop, Parcels: []row{}}
	for _, p := range ps {
		list.Parcels = append(list.Parcels, row{p.Reference, p.Status, p.ServiceLevel, p.WeightGrams})
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(list); err != nil {
		fmt.Fprintln(errOut, "parcels admin:", err)
		return 1
	}
	return 0
}
