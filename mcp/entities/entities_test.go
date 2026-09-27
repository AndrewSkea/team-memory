package entities

import (
	"reflect"
	"testing"
)

func TestMergeAndCanonical(t *testing.T) {
	var r Registry
	if !r.Merge([]Entity{{Name: "Payments API", Aliases: []string{"payments-svc", "@acme/payments-client"}}}) {
		t.Fatal("expected change")
	}
	if r.Entities[0].Name != "payments-api" {
		t.Fatalf("name %q", r.Entities[0].Name)
	}
	if c, ok := r.Canonical("Payments_Svc"); !ok || c != "payments-api" {
		t.Fatalf("canonical %q %v", c, ok)
	}
	// Re-merging the same thing via an alias is a no-op.
	if r.Merge([]Entity{{Name: "payments svc", Aliases: []string{"payments-api"}}}) {
		t.Fatal("expected no change")
	}
	if !r.Merge([]Entity{{Name: "payments-svc", Aliases: []string{"billing gateway"}}}) {
		t.Fatal("expected alias add")
	}
	if len(r.Entities) != 1 || len(r.Entities[0].Aliases) != 3 {
		t.Fatalf("%+v", r.Entities)
	}
	parsed, err := Parse([]byte(r.Render()))
	if err != nil || !reflect.DeepEqual(parsed, r) {
		t.Fatalf("render/parse: %v %+v", err, parsed)
	}
}

func TestMatcher(t *testing.T) {
	r := Registry{Entities: []Entity{
		{Name: "payments-api", Aliases: []string{"PAYMENTS_API_URL", "@acme/payments-client"}},
		{Name: "kafka"},
		{Name: "go"}, // too short: ignored to avoid noise
	}}
	m := NewMatcher(r, []string{"Order Service"})
	got := m.Match("Why does checkout time out when calling PAYMENTS_API_URL? Also the order-service and Kafka lag. go go")
	want := []string{"kafka", "order-service", "payments-api"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if got := m.Match("the kafkaesque process"); len(got) != 0 {
		t.Fatalf("partial word matched: %v", got)
	}
}
