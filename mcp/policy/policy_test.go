package policy

import "testing"

func TestEffectiveClampsToMinimum(t *testing.T) {
	p := Default()
	if p.Effective("") != AI {
		t.Fatal("default should be ai")
	}
	if p.Effective("auto") != Auto {
		t.Fatal("user may pick auto when minimum is auto")
	}
	p.MinimumMode = AI
	if p.Effective("auto") != AI {
		t.Fatal("auto must clamp to ai")
	}
	if p.Effective("review") != Review {
		t.Fatal("stricter is allowed")
	}
	if p.Effective("bogus") != AI {
		t.Fatal("bogus falls back to default")
	}
	if len(p.Allowed()) != 4 {
		t.Fatalf("allowed %v", p.Allowed())
	}
}

func TestParseFillsDefaults(t *testing.T) {
	p, err := Parse([]byte("minimum_mode: review\nalways_review_types: [decision]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if p.MinimumMode != Review || p.DefaultMode != AI || p.ReviewTimeoutDays != 3 || p.OutsideOrg != "review" {
		t.Fatalf("%+v", p)
	}
	if _, err := Parse([]byte(p.Render())); err != nil {
		t.Fatal(err)
	}
}

func TestDecide(t *testing.T) {
	p := Default()
	p.AlwaysReviewTypes = []string{"decision"}
	good := Candidate{Type: "gotcha", Sensitivity: "shareable", Confidence: 0.9}
	cases := []struct {
		name  string
		mode  Mode
		c     Candidate
		inOrg bool
		want  Action
	}{
		{"ai confident", AI, good, true, Publish},
		{"ai low conf", AI, Candidate{Type: "gotcha", Sensitivity: "shareable", Confidence: 0.5}, true, Hold},
		{"ai contradicts", AI, Candidate{Type: "gotcha", Sensitivity: "shareable", Confidence: 0.9, Contradicts: true}, true, Hold},
		{"ai redacted", AI, Candidate{Type: "gotcha", Sensitivity: "shareable", Confidence: 0.9, Redacted: true}, true, Hold},
		{"ai decision type", AI, Candidate{Type: "Decision", Sensitivity: "shareable", Confidence: 0.99}, true, Hold},
		{"personal dropped", Auto, Candidate{Sensitivity: "personal", Confidence: 1}, true, Drop},
		{"sensitive dropped", AI, Candidate{Sensitivity: "sensitive", Confidence: 1}, true, Drop},
		{"auto publishes", Auto, Candidate{Sensitivity: "shareable", Confidence: 0.1}, true, Publish},
		{"review holds", Review, good, true, Hold},
		{"review-timeout holds", ReviewTimeout, good, true, Hold},
		{"manual drops", Manual, good, true, Drop},
		{"outside org held", Auto, good, false, Hold},
	}
	for _, c := range cases {
		if got := p.Decide(c.mode, c.c, c.inOrg); got.Action != c.want {
			t.Errorf("%s: got %s (%s), want %s", c.name, got.Action, got.Reason, c.want)
		}
	}
	p.OutsideOrg = "skip"
	if p.Decide(AI, good, false).Action != Drop {
		t.Error("outside_org skip")
	}
	p.OutsideOrg = "allow"
	if p.Decide(AI, good, false).Action != Publish {
		t.Error("outside_org allow")
	}
}

func TestInOrg(t *testing.T) {
	p := Default()
	p.Orgs = []string{"acme-labs"}
	if !p.InOrg("Acme", "acme") || !p.InOrg("acme", "ACME-LABS") || p.InOrg("acme", "alice") || p.InOrg("acme", "") {
		t.Fatal("InOrg")
	}
}
