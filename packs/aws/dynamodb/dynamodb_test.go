package awsdynamodb

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep"
)

// An item's values keep their types (ADR 0014): a number is the number of
// its text, and a string that reads as a number a string.
func TestItemValuesKeepTheirTypes(t *testing.T) {
	item := map[string]types.AttributeValue{
		"declaredValue": &types.AttributeValueMemberN{Value: "120"},
		"weightKg":      &types.AttributeValueMemberN{Value: "2.50"},
		"odd":           &types.AttributeValueMemberN{Value: "+5"},
		"postcode":      &types.AttributeValueMemberS{Value: "37073"},
		"signed":        &types.AttributeValueMemberBOOL{Value: true},
		"note":          &types.AttributeValueMemberNULL{Value: true},
		"sizes":         &types.AttributeValueMemberNS{Value: []string{"1", "2.5"}},
		"tags":          &types.AttributeValueMemberSS{Value: []string{"fragile"}},
		"address":       &types.AttributeValueMemberM{Value: map[string]types.AttributeValue{"floor": &types.AttributeValueMemberN{Value: "3"}}},
	}
	got := itemJSON(item)
	want := `{"address":{"floor":3},"declaredValue":120,"note":null,"odd":"+5","postcode":"37073","signed":true,"sizes":[1,2.5],"tags":["fragile"],"weightKg":2.50}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	rs := cloudstep.Rows{
		{Key: "declaredValue", Value: "120"},
		{Key: "weightKg", Value: "2.5"},
		{Key: "postcode", Value: `"37073"`},
		{Key: "address.floor", Value: "3"},
		{Key: "note", Value: "null"},
		{Key: "missing", Value: "undefined"},
	}
	if ok, err := rs.Match(got); !ok || err != nil {
		t.Errorf("the item should meet the conditions: %v", err)
	}
	for _, r := range []core.Pair{{Key: "declaredValue", Value: `"120"`}, {Key: "postcode", Value: "37073"}, {Key: "weightKg", Value: "2"}} {
		if ok, _ := (cloudstep.Rows{r}).Match(got); ok {
			t.Errorf("%s %s should not match a value of another type", r.Key, r.Value)
		}
	}
}
