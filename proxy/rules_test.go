package main

import (
	"os"
	"testing"
)

func TestRulesMatch(t *testing.T) {
	r := NewRules(
		"ads.example.com",                // block
		".sub-1.example.com, *.vpn.test", // vpn
		".example.com,exact.org",         // direct
		Block,
	)
	cases := []struct {
		host string
		want Action
	}{
		{"sub-1.example.com", VPN},    // ".x" matches the domain itself
		{"a.sub-1.example.com", VPN},  // and its subdomains
		{"A.Sub-1.Example.COM.", VPN}, // case and trailing dot are ignored
		{"b.vpn.test", VPN},           // "*.x" behaves like ".x"
		{"www.example.com", Direct},   // shorter suffix rule
		{"ads.example.com", Block},    // longer exact rule beats the suffix rule
		{"x.ads.example.com", Direct}, // exact rule does not cover subdomains
		{"exact.org", Direct},         // exact rule
		{"www.exact.org", Block},      // ...but not its subdomains -> default
		{"notexample.com", Block},     // suffix match is on label boundaries
		{"other.net", Block},          // default
		{"10.0.0.1", Block},           // IP literals fall through to the default
	}
	for _, c := range cases {
		if got := r.Match(c.host); got != c.want {
			t.Errorf("Match(%q) = %s, want %s", c.host, got, c.want)
		}
	}
}

func TestRulesTieBreak(t *testing.T) {
	// Same entry in several lists: block wins over vpn, vpn over direct.
	r := NewRules(".x.com", ".x.com", ".x.com", Direct)
	if got := r.Match("a.x.com"); got != Block {
		t.Errorf("block/vpn/direct tie = %s, want block", got)
	}
	r = NewRules("", ".x.com", ".x.com", Block)
	if got := r.Match("a.x.com"); got != VPN {
		t.Errorf("vpn/direct tie = %s, want vpn", got)
	}
}

func TestParseAction(t *testing.T) {
	for in, want := range map[string]Action{"": Block, "block": Block, " Direct ": Direct, "VPN": VPN} {
		got, err := parseAction(in)
		if err != nil || got != want {
			t.Errorf("parseAction(%q) = %s, %v; want %s", in, got, err, want)
		}
	}
	if _, err := parseAction("proxy"); err == nil {
		t.Error("parseAction(\"proxy\") should fail")
	}
}

func TestReadNameservers(t *testing.T) {
	path := t.TempDir() + "/resolv.conf"
	data := "# comment\nsearch lan\nnameserver 10.0.0.1\nnameserver  8.8.8.8 \noptions ndots:1\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readNameservers(path)
	if len(got) != 2 || got[0] != "10.0.0.1" || got[1] != "8.8.8.8" {
		t.Errorf("readNameservers = %v", got)
	}
	if got := readNameservers(path + ".missing"); got != nil {
		t.Errorf("missing file = %v, want nil", got)
	}
}
