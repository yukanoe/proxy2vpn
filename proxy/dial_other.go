//go:build !linux

package main

import (
	"errors"
	"syscall"
)

func socketControl(iface string, mark int) func(network, address string, c syscall.RawConn) error {
	return func(string, string, syscall.RawConn) error {
		return errors.New("binding sockets to an interface is not supported on this OS yet")
	}
}
