package main

import (
	"fmt"
	"strings"
)

type Action int

const (
	Block Action = iota
	Direct
	VPN
)

func (a Action) String() string {
	switch a {
	case Direct:
		return "direct"
	case VPN:
		return "vpn"
	default:
		return "block"
	}
}

func parseAction(s string) (Action, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "block":
		return Block, nil
	case "direct":
		return Direct, nil
	case "vpn":
		return VPN, nil
	}
	return Block, fmt.Errorf("invalid action %q (want block|direct|vpn)", s)
}

type rule struct {
	name   string // lower-case domain without a leading dot
	suffix bool   // true: matches the domain and all of its subdomains
	action Action
}

// Rules picks an action for a host. Entry syntax:
//
//	.example.com or *.example.com -> example.com and every subdomain
//	example.com                   -> example.com only
//
// The longest matching entry wins; on a tie the earlier list wins (block > vpn > direct).
type Rules struct {
	rules []rule
	def   Action
}

func NewRules(block, vpn, direct string, def Action) *Rules {
	r := &Rules{def: def}
	r.add(block, Block)
	r.add(vpn, VPN)
	r.add(direct, Direct)
	return r
}

func (r *Rules) add(list string, a Action) {
	for _, e := range strings.FieldsFunc(list, func(c rune) bool { return c == ',' || c == ' ' }) {
		e = strings.ToLower(strings.TrimSuffix(e, "."))
		ru := rule{action: a}
		switch {
		case strings.HasPrefix(e, "*."):
			ru.name, ru.suffix = e[2:], true
		case strings.HasPrefix(e, "."):
			ru.name, ru.suffix = e[1:], true
		default:
			ru.name = e
		}
		if ru.name != "" {
			r.rules = append(r.rules, ru)
		}
	}
}

func (r *Rules) Match(host string) Action {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	best, act := -1, r.def
	for _, ru := range r.rules {
		if host == ru.name || (ru.suffix && strings.HasSuffix(host, "."+ru.name)) {
			if len(ru.name) > best {
				best, act = len(ru.name), ru.action
			}
		}
	}
	return act
}

func (r *Rules) String() string {
	var b strings.Builder
	for _, ru := range r.rules {
		if ru.suffix {
			b.WriteString(".")
		}
		fmt.Fprintf(&b, "%s=%s ", ru.name, ru.action)
	}
	fmt.Fprintf(&b, "(default=%s)", r.def)
	return b.String()
}
