package main

import (
	"errors"
	"testing"
)

func TestIpsecReason(t *testing.T) {
	noProp := `initiating Main Mode IKE_SA L2TP[2] to 106.184.145.102
generating ID_PROT request 0 [ SA V V V V V ]
sending packet: from 10.89.0.5[500] to 106.184.145.102[500] (284 bytes)
received packet: from 106.184.145.102[500] to 10.89.0.5[500] (56 bytes)
parsed INFORMATIONAL_V1 request 0 [ N(NO_PROP) ]
received NO_PROPOSAL_CHOSEN error notify
establishing connection 'L2TP' failed`
	timeout := `initiating Main Mode IKE_SA L2TP[1] to 192.0.2.1
sending retransmit 5 of request message ID 0, seq 1
giving up after 5 retransmits
establishing IKE_SA failed, peer not responding
establishing connection 'L2TP' failed`

	cases := []struct {
		name, out string
		err       error
		want      string
	}{
		{"no proposal", noProp, nil, "received NO_PROPOSAL_CHOSEN error notify"},
		{"no response", timeout, nil, "giving up after 5 retransmits"},
		{"command error", "", errors.New("exit status 1"), "exit status 1"},
		{"unknown", "something odd", nil, "something odd"},
	}
	for _, c := range cases {
		if got := ipsecReason(c.out, c.err); got != c.want {
			t.Errorf("%s: ipsecReason = %q, want %q", c.name, got, c.want)
		}
	}
}
