// Package awscore is the aws-core pack: the AWS account the aws-* packs
// talk to, set up as the AWS SDK sets it up for the real services, and the
// SDK configuration those packs build their clients from.
package awscore

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/nimbusxr/axx/core"
)

// Name is the pack's name; the aws-* packs require it.
const Name = "aws-core"

const packDoc = `The AWS account the aws-* packs talk to, set up the way the AWS SDK is set up for the real services.

Register the account once with ` + "`the {word} aws account with the following properties:`" + `; every aws-* step of the scenario uses it (the first account registered is the default).

| Property | |
| --- | --- |
| ` + "`region`" + ` | required, e.g. ` + "`eu-west-1`" + ` |
| ` + "`endpoint`" + ` | where every service of the account is, instead of AWS: a local emulator such as ` + "`http://localhost:4566`" + ` (S3 is then addressed by path) |
| ` + "`profile`" + ` | a profile of the shared AWS config and credentials files |
| ` + "`access key id`" + `, ` + "`secret access key`" + `, ` + "`session token`" + ` | static credentials |

Without credentials in the table, the SDK finds them as it always does: the ` + "`AWS_*`" + ` environment variables, the shared files, then the container or instance role. ` + "`AWS_ENDPOINT_URL`" + ` is honored too, so the same features run against AWS and against an emulator. Values expand ` + "`${env:..}`" + ` and ` + "`${sys:..}`" + `.`

// Pack returns the aws-core pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      Name,
		Namespace: Name,
		Doc:       packDoc,
		Steps: []core.StepDef{{
			ID: "aws-core.account", Keyword: "Given", Arg: core.ArgTable, Since: "0.1.0",
			Expr: "the {word} aws account with the following properties:",
			Doc: "Register the AWS account the aws-* steps talk to: `region` (required), `endpoint` (an emulator), `profile`, " +
				"or `access key id` and `secret access key` (and `session token`). Without credentials the SDK's default chain is used.",
			Examples: []string{"Given the parcels aws account with the following properties:"},
			Run: func(sc *core.Scenario, a core.Args) error {
				acct, err := Parse(sc.Suite(), a.String(0), a.Table)
				if err != nil {
					return err
				}
				return accounts.Of(sc).Add(acct.Name, acct)
			},
		}},
	}
}

// Account is a registered AWS account.
type Account struct {
	Name     string
	Region   string
	Endpoint string
	Profile  string

	accessKey, secretKey, sessionToken string
}

// Key identifies the account's configuration, for resources shared by the
// scenarios of a run (listeners, clients).
func (a *Account) Key() string {
	return strings.Join([]string{a.Region, a.Endpoint, a.Profile, a.accessKey}, "|")
}

// Config is the SDK configuration of the account, loaded once per run.
func (a *Account) Config(ctx context.Context, s *core.Suite) (aws.Config, error) {
	return core.Cached(s, "aws-core/config/"+a.Key(), func() (aws.Config, error) {
		opts := []func(*config.LoadOptions) error{config.WithRegion(a.Region)}
		if a.Profile != "" {
			opts = append(opts, config.WithSharedConfigProfile(a.Profile))
		}
		if a.accessKey != "" {
			opts = append(opts, config.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(a.accessKey, a.secretKey, a.sessionToken)))
		}
		if a.Endpoint != "" {
			opts = append(opts, config.WithBaseEndpoint(a.Endpoint))
		}
		cfg, err := config.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			return aws.Config{}, fmt.Errorf("cannot load the AWS configuration of the %s aws account: %w", a.Name, err)
		}
		return cfg, nil
	})
}

var properties = []string{"region", "endpoint", "profile", "access key id", "secret access key", "session token"}

// Parse reads an account's properties (expanding ${env:..} and ${sys:..}).
// The aws-* packs also use it to read the accounts of a planned run.
func Parse(s *core.Suite, name string, t *core.Table) (*Account, error) {
	pairs, err := t.Pairs()
	if err != nil {
		return nil, err
	}
	a := &Account{Name: name}
	for _, p := range pairs {
		v := strings.TrimSpace(s.Interpolate(p.Value))
		switch p.Key {
		case "region":
			a.Region = v
		case "endpoint":
			a.Endpoint = strings.TrimRight(v, "/")
		case "profile":
			a.Profile = v
		case "access key id":
			a.accessKey = v
		case "secret access key":
			a.secretKey = v
		case "session token":
			a.sessionToken = v
		default:
			return nil, fmt.Errorf("unknown aws account property %q (supported: %s)", p.Key, strings.Join(properties, ", "))
		}
	}
	if a.Region == "" {
		return nil, fmt.Errorf(`the aws account property "region" is required`)
	}
	if (a.accessKey == "") != (a.secretKey == "") {
		return nil, fmt.Errorf(`the aws account properties "access key id" and "secret access key" go together`)
	}
	return a, nil
}

var accounts = core.NewStateKey(Name, func(*core.Scenario) *core.Services[*Account] {
	return core.NewServices[*Account]("AWS account",
		`No AWS account is registered in this scenario; register one with "the {word} aws account with the following properties:"`)
}, nil)

// Default returns the scenario's AWS account (the first registered).
func Default(sc *core.Scenario) (*Account, error) { return accounts.Of(sc).Default() }

// Planned returns, for each planned scenario, the AWS account it registers
// first, so packs can prepare what a run's steps will need before the
// scenarios start. Scenarios that register none are left out.
func Planned(s *core.Suite, plan *core.Plan) map[string]*Account {
	out := map[string]*Account{}
	for _, sc := range plan.Scenarios {
		for _, st := range sc.Steps {
			if st.Definition != "aws-core.account" {
				continue
			}
			if a, err := Parse(s, st.Args[0].Raw, st.Table); err == nil {
				out[sc.ID] = a
			}
			break
		}
	}
	return out
}
