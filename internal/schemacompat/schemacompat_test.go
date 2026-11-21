package schemacompat

import (
	"strings"
	"testing"
)

const before = `{
  "$defs": {
    "Service": {
      "type": "object",
      "additionalProperties": false,
      "required": ["command"],
      "properties": {
        "command": {"oneOf": [{"type": "string"}, {"type": "array", "items": {"type": "string"}, "minItems": 1}]},
        "mode": {"type": "string", "enum": ["fail", "warn"]},
        "retries": {"type": "integer", "minimum": 0, "maximum": 10},
        "after": {"type": "array", "items": {"$ref": "#/$defs/Service"}}
      }
    }
  },
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "version": {"type": "integer", "enum": [1]},
    "services": {"type": "object", "additionalProperties": {"$ref": "#/$defs/Service"}},
    "packs": {"type": "object", "additionalProperties": true}
  }
}`

func TestASchemaMayGrow(t *testing.T) {
	for name, now := range map[string]string{
		"the same":                before,
		"a new property":          strings.Replace(before, `"version": {`, `"docs": {"type": "string"}, "version": {`, 1),
		"a new enum value":        strings.Replace(before, `["fail", "warn"]`, `["fail", "warn", "off"]`, 1),
		"a looser limit":          strings.Replace(before, `"maximum": 10`, `"maximum": 20`, 1),
		"a limit gone":            strings.Replace(before, `, "minimum": 0`, ``, 1),
		"a new form":              strings.Replace(before, `{"type": "string"}, {"type": "array"`, `{"type": "string"}, {"type": "object"}, {"type": "array"`, 1),
		"no longer required":      strings.Replace(before, `"required": ["command"],`, ``, 1),
		"a description":           strings.Replace(before, `"type": "integer", "enum": [1]`, `"type": "integer", "enum": [1], "description": "the file's version"`, 1),
		"a renamed definition":    strings.ReplaceAll(strings.Replace(before, `"Service": {`, `"Svc": {`, 1), `#/$defs/Service`, `#/$defs/Svc`),
		"a number for an integer": strings.Replace(before, `"retries": {"type": "integer"`, `"retries": {"type": "number"`, 1),
	} {
		got, err := Breaks([]byte(before), []byte(now))
		if err != nil || len(got) != 0 {
			t.Errorf("%s: %v %v, want no break", name, got, err)
		}
	}
}

func TestASchemaMayNotTakeBackWhatAFileCouldSay(t *testing.T) {
	for name, c := range map[string]struct{ now, want string }{
		"a property gone":        {strings.Replace(before, `"packs": {"type": "object", "additionalProperties": true}`, `"x": {}`, 1), "packs: the property is gone"},
		"a nested property gone": {strings.Replace(before, `"mode": {"type": "string", "enum": ["fail", "warn"]},`, ``, 1), "services.additionalProperties.mode: the property is gone"},
		"an enum value gone":     {strings.Replace(before, `["fail", "warn"]`, `["fail"]`, 1), `services.additionalProperties.mode: it no longer takes "warn"`},
		"another type":           {strings.Replace(before, `"version": {"type": "integer"`, `"version": {"type": "string"`, 1), "version: it took integer, and takes string now"},
		"newly required":         {strings.Replace(before, `"required": ["command"]`, `"required": ["command", "mode"]`, 1), `services.additionalProperties: it requires "mode" now`},
		"a tighter limit":        {strings.Replace(before, `"maximum": 10`, `"maximum": 5`, 1), "services.additionalProperties.retries: its maximum is 5 now, and was 10"},
		"a new limit":            {strings.Replace(before, `"minItems": 1`, `"minItems": 1, "maxItems": 3`, 1), "its maxItems is 3 now, and was not set"},
		"a form gone":            {strings.Replace(before, `{"type": "string"}, {"type": "array"`, `{"type": "array"`, 1), "services.additionalProperties.command.oneOf[0]"},
		"closed":                 {strings.Replace(before, `"packs": {"type": "object", "additionalProperties": true}`, `"packs": {"type": "object", "additionalProperties": false}`, 1), "packs: it takes no property it does not name now, and took others"},
		"a free value held":      {strings.Replace(before, `"packs": {"type": "object", "additionalProperties": true}`, `"packs": {"type": "object", "additionalProperties": true, "properties": {"rest": {"type": "object"}}}`, 1), "packs.rest: the property is new in an object that took any property"},
		"an unread keyword":      {strings.Replace(before, `"version": {"type": "integer"`, `"version": {"multipleOf": 2, "type": "integer"`, 1), "version: its multipleOf changed, a keyword this check does not read"},
		"through a reference":    {strings.Replace(before, `"items": {"$ref": "#/$defs/Service"}`, `"items": {"type": "string"}`, 1), "services.additionalProperties.after.items"},
	} {
		got, err := Breaks([]byte(before), []byte(c.now))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !strings.Contains(strings.Join(got, "\n"), c.want) {
			t.Errorf("%s: got\n  %s\nwant a line with %q", name, strings.Join(got, "\n  "), c.want)
		}
	}
}

func TestASchemaThatNamesItselfIsCompared(t *testing.T) {
	got, err := Breaks([]byte(before), []byte(before))
	if err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
}
