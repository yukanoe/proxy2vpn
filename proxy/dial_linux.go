package main

import "syscall"

// socketControl binds a socket to an interface (SO_BINDTODEVICE) and sets its mark (SO_MARK).
// Binding is the kill-switch: if the VPN drops, ppp0 disappears and connect fails
// instead of leaking out through eth0.
func socketControl(iface string, mark int) func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			if iface != "" {
				if serr = syscall.BindToDevice(int(fd), iface); serr != nil {
					return
				}
			}
			if mark != 0 {
				serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, mark)
			}
		})
		if err != nil {
			return err
		}
		return serr
	}
}
