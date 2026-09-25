package jvalue_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/nimbusxr/axx/internal/compat/internal/oracletest"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
)

// errOf converts jvalue errors to the oracle's exception form.
func errOf(err error) *oracletest.JavaError {
	var se *jvalue.StepError
	if errors.As(err, &se) {
		m := se.Message
		return &oracletest.JavaError{Class: "StepError", Message: &m}
	}
	return oracletest.ErrorOf(err)
}

type valueCase struct {
	Input  string                `json:"input"`
	Result *string               `json:"result"`
	Error  *oracletest.JavaError `json:"error"`
}

type restOracle struct {
	Infer   []valueCase `json:"infer"`
	Extract []struct {
		Path   string `json:"path"`
		Parent string `json:"parent"`
		Key    string `json:"key"`
	} `json:"extract"`
	RequestDocs map[string]string `json:"requestDocs"`
	Request     []struct {
		Doc    string                `json:"doc"`
		Path   string                `json:"path"`
		Value  string                `json:"value"`
		Mode   string                `json:"mode"`
		Result *string               `json:"result"`
		Typed  *string               `json:"typed"`
		Error  *oracletest.JavaError `json:"error"`
	} `json:"request"`
	Coerce         []valueCase       `json:"coerce"`
	ResponseBodies map[string]string `json:"responseBodies"`
	Response       []struct {
		Body    string                `json:"body"`
		Path    string                `json:"path"`
		Value   string                `json:"value"`
		Kind    string                `json:"kind"`
		Outcome *string               `json:"outcome"`
		Error   *oracletest.JavaError `json:"error"`
	} `json:"response"`
	Form []struct {
		JSON   string                `json:"json"`
		Result *string               `json:"result"`
		Error  *oracletest.JavaError `json:"error"`
	} `json:"form"`
}

func checkValue(t *testing.T, oracle, key string, c valueCase, v any, err error, compare func(a, b *oracletest.JavaError) bool) {
	t.Helper()
	if c.Error != nil || err != nil {
		got := errOf(err)
		oracletest.Check(t, oracle, key, compare(got, c.Error), func() string {
			return fmt.Sprintf("want error %v, got %v (value %s)", c.Error, got, oracletest.Repr(v))
		})
		return
	}
	got := oracletest.Repr(v)
	oracletest.Check(t, oracle, key, got == *c.Result, func() string {
		return fmt.Sprintf("want %s, got %s", *c.Result, got)
	})
}

func TestRestStepsOracle(t *testing.T) {
	var o restOracle
	oracletest.Load(t, "reststeps", &o)
	if len(o.Request) < 1000 || len(o.Response) < 300 {
		t.Fatalf("oracle too small: %d request, %d response cases", len(o.Request), len(o.Response))
	}
	for _, c := range o.Infer {
		checkValue(t, "reststeps", "infer|"+c.Input, c, jvalue.InferRequestValue(c.Input), nil, oracletest.SameError)
	}
	for _, c := range o.Extract {
		parent, key := jsonx.ParentAndKey(c.Path)
		oracletest.Check(t, "reststeps", "extract|"+c.Path, parent == c.Parent && key == c.Key, func() string {
			return fmt.Sprintf("want (%s, %s), got (%s, %s)", c.Parent, c.Key, parent, key)
		})
	}
	for _, c := range o.Request {
		key := fmt.Sprintf("request|%s|%s|%s|%s", c.Doc, c.Mode, c.Path, c.Value)
		doc, err := jsonx.Parse(o.RequestDocs[c.Doc])
		if err != nil {
			t.Fatal(err)
		}
		switch c.Mode {
		case "single":
			err = jvalue.SetRequestProperty(doc, c.Path, c.Value)
		case "table":
			err = jvalue.ApplyRequestTableRow(doc, c.Path, c.Value)
		case "null":
			err = jvalue.SetRequestPropertyNull(doc, c.Path)
		case "delete":
			err = jvalue.DeleteRequestProperty(doc, c.Path)
		}
		if c.Error != nil || err != nil {
			got := errOf(err)
			oracletest.Check(t, "reststeps", key, oracletest.SameError(got, c.Error), func() string {
				s, _ := jsonx.Marshal(doc)
				return fmt.Sprintf("want error %v\n got %v (doc %s)", c.Error, got, s)
			})
			continue
		}
		s, err := jsonx.Marshal(doc)
		typed := oracletest.Repr(doc)
		oracletest.Check(t, "reststeps", key, err == nil && s == *c.Result && typed == *c.Typed, func() string {
			return fmt.Sprintf("want %s\n got %s (err %v)\nwant typed %s\n got typed %s", *c.Result, s, err, *c.Typed, typed)
		})
	}
	for _, c := range o.Coerce {
		v, err := jvalue.CoerceExpected(c.Input)
		checkValue(t, "reststeps", "coerce|"+c.Input, c, v, err, oracletest.SameError)
	}
	for _, c := range o.Response {
		key := fmt.Sprintf("response|%s|%s|%s|%s", c.Body, c.Kind, c.Path, c.Value)
		body := o.ResponseBodies[c.Body]
		var ok bool
		var err error
		switch c.Kind {
		case "is":
			ok, err = jvalue.ResponsePropertyIs(body, c.Path, c.Value)
		case "null":
			ok, err = jvalue.ResponsePropertyIsNull(body, c.Path)
		case "undefined":
			ok, err = jvalue.ResponsePropertyIsUndefined(body, c.Path)
		case "matches":
			ok, err = jvalue.ResponsePropertyMatches(body, c.Path, c.Value)
		case "table":
			ok, err = jvalue.ResponseTableRow(body, c.Path, c.Value)
		}
		checkOutcome(t, "reststeps", key, c.Outcome, c.Error, ok, err, sameClassForRegex)
	}
	for _, c := range o.Form {
		s, err := jvalue.FormURLEncode(c.JSON)
		key := "form|" + c.JSON
		if c.Error != nil || err != nil {
			got := errOf(err)
			oracletest.Check(t, "reststeps", key, oracletest.SameError(got, c.Error), func() string {
				return fmt.Sprintf("want error %v, got %v (%s)", c.Error, got, s)
			})
			continue
		}
		oracletest.Check(t, "reststeps", key, s == *c.Result, func() string {
			return fmt.Sprintf("want %s\n got %s", *c.Result, s)
		})
	}
	oracletest.CheckAllDeviationsSeen(t, "reststeps")
}

// sameClassForRegex compares messages exactly except for regex syntax errors,
// whose messages come from the regex parser's wording.
func sameClassForRegex(a, b *oracletest.JavaError) bool {
	if a != nil && b != nil && a.Class == "PatternSyntaxException" {
		return a.Class == b.Class
	}
	return oracletest.SameError(a, b)
}

func checkOutcome(t *testing.T, oracle, key string, want *string, wantErr *oracletest.JavaError, ok bool, err error,
	compare func(a, b *oracletest.JavaError) bool,
) {
	t.Helper()
	if wantErr != nil || err != nil {
		got := errOf(err)
		oracletest.Check(t, oracle, key, compare(got, wantErr), func() string {
			return fmt.Sprintf("want error %v, got %v (outcome %v)", wantErr, got, ok)
		})
		return
	}
	got := "fail"
	if ok {
		got = "pass"
	}
	oracletest.Check(t, oracle, key, got == *want, func() string {
		return fmt.Sprintf("want %s, got %s", *want, got)
	})
}

type kafkaOracle struct {
	Coerce []valueCase       `json:"coerce"`
	Bodies map[string]string `json:"bodies"`
	Cases  []struct {
		Body    string                `json:"body"`
		Path    string                `json:"path"`
		Value   string                `json:"value"`
		Outcome *string               `json:"outcome"`
		Error   *oracletest.JavaError `json:"error"`
	} `json:"cases"`
}

func TestKafkaOracle(t *testing.T) {
	var o kafkaOracle
	oracletest.Load(t, "kafka", &o)
	for _, c := range o.Coerce {
		v, err := jvalue.CoerceKafkaExpected(c.Input)
		if c.Result != nil && *c.Result == "nullValue()" {
			oracletest.Check(t, "kafka", "coerce|"+c.Input, err == nil && v == nil, func() string {
				return fmt.Sprintf("want nullValue(), got %s %v", oracletest.Repr(v), err)
			})
			continue
		}
		checkValue(t, "kafka", "coerce|"+c.Input, c, v, err, oracletest.SameError)
	}
	for _, c := range o.Cases {
		ok, err := jvalue.KafkaPropertyMatches(o.Bodies[c.Body], c.Path, c.Value)
		checkOutcome(t, "kafka", fmt.Sprintf("case|%s|%s|%s", c.Body, c.Path, c.Value), c.Outcome, c.Error, ok, err, oracletest.SameError)
	}
	oracletest.CheckAllDeviationsSeen(t, "kafka")
}

type postgresOracle struct {
	AsText []struct {
		Input    string                `json:"input"`
		NodeType string                `json:"nodeType"`
		Result   *string               `json:"result"`
		Error    *oracletest.JavaError `json:"error"`
	} `json:"asText"`
	Stringify []struct {
		Input  string                `json:"input"`
		Result *string               `json:"result"`
		Error  *oracletest.JavaError `json:"error"`
	} `json:"stringify"`
	Columns map[string]string `json:"columns"`
	Cases   []struct {
		Column  string `json:"column"`
		Path    string `json:"path"`
		Value   string `json:"value"`
		Kind    string `json:"kind"`
		Outcome *string
		Error   *struct {
			Class   string `json:"class"`
			Message string `json:"message"`
			Cause   string `json:"cause"`
		} `json:"error"`
	} `json:"cases"`
}

func TestPostgresOracle(t *testing.T) {
	var o postgresOracle
	oracletest.Load(t, "postgres", &o)
	for _, c := range o.AsText {
		v, err := jvalue.ParseJackson(c.Input)
		key := "asText|" + c.Input
		if c.Error != nil || err != nil {
			oracletest.Check(t, "postgres", key, (c.Error != nil) == (err != nil), func() string {
				return fmt.Sprintf("want error %v, got %v (value %s)", c.Error, err, oracletest.Repr(v))
			})
			continue
		}
		got := jvalue.Stringify(v)
		oracletest.Check(t, "postgres", key, got == *c.Result, func() string {
			return fmt.Sprintf("want %q, got %q", *c.Result, got)
		})
	}
	for _, c := range o.Stringify {
		s, err := jvalue.StringifyJSON(c.Input)
		key := "stringify|" + c.Input
		if c.Error != nil || err != nil {
			oracletest.Check(t, "postgres", key, (c.Error != nil) == (err != nil), func() string {
				return fmt.Sprintf("want error %v, got %v (%s)", c.Error, err, s)
			})
			continue
		}
		oracletest.Check(t, "postgres", key, s == *c.Result, func() string {
			return fmt.Sprintf("want %s\n got %s", *c.Result, s)
		})
	}
	for _, c := range o.Cases {
		col := o.Columns[c.Column]
		var ok bool
		var err error
		if c.Kind == "are" {
			ok, err = jvalue.PostgresPropertyIs(col, c.Path, c.Value)
		} else {
			ok, err = jvalue.PostgresPropertyMatches(col, c.Path, c.Value)
		}
		key := fmt.Sprintf("case|%s|%s|%s|%s", c.Column, c.Kind, c.Path, c.Value)
		if c.Error != nil || err != nil {
			var se *jvalue.StepError
			isStep := errors.As(err, &se)
			oracletest.Check(t, "postgres", key, c.Error != nil && isStep && se.Message == c.Error.Message, func() string {
				return fmt.Sprintf("want error %+v, got %v (outcome %v)", c.Error, err, ok)
			})
			continue
		}
		got := "fail"
		if ok {
			got = "pass"
		}
		oracletest.Check(t, "postgres", key, got == *c.Outcome, func() string {
			return fmt.Sprintf("want %s, got %s", *c.Outcome, got)
		})
	}
	oracletest.CheckAllDeviationsSeen(t, "postgres")
}
