package bgp2etcd

import "net/netip"

type UpdateMessage struct {
	Full      bool
	Additions []Route
	Deletions []Route
}

type Route struct {
	Prefix  netip.Prefix
	NextHop netip.Addr
}
