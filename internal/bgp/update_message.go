package bgp

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/jwhited/corebgp"
)

type updateMessage struct {
	addPathIPv4      bool
	withdrawn        []netip.Prefix
	addPathWithdrawn []corebgp.AddPathPrefix
	origin           uint8
	asPath           []uint32
	nextHop          netip.Addr
	communities      []uint32
	nlri             []netip.Prefix
	addPathNLRI      []corebgp.AddPathPrefix
}

func fmtSlice[T any](t []T, name string, sb *strings.Builder) {
	if len(t) > 0 {
		if sb.Len() > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(fmt.Sprintf("%s=%v", name, t))
	}
}

func (u updateMessage) String() string {
	commsFmt := func(in []uint32) []string {
		comms := make([]string, 0, len(in))
		for _, c := range in {
			comms = append(comms, fmt.Sprintf("%d:%d", c>>16, c&0x0000FFFF))
		}
		return comms
	}
	var sb strings.Builder
	fmtSlice[netip.Prefix](u.nlri, "nlri", &sb)
	fmtSlice[corebgp.AddPathPrefix](u.addPathNLRI, "addPathNLRI", &sb)
	if len(u.nlri) > 0 || len(u.addPathNLRI) > 0 {
		sb.WriteString(fmt.Sprintf(" origin=%v", u.origin))
		if len(u.nlri) > 0 {
			sb.WriteString(fmt.Sprintf(" nextHop=%v", u.nextHop))
		}
	}
	fmtSlice[uint32](u.asPath, "asPath", &sb)
	fmtSlice[string](commsFmt(u.communities), "communities", &sb)
	fmtSlice[netip.Prefix](u.withdrawn, "withdrawn", &sb)
	fmtSlice[corebgp.AddPathPrefix](u.addPathWithdrawn, "addPathWithdrawn", &sb)
	if sb.Len() == 0 {
		return "End-of-RIB"
	}
	return sb.String()
}

func newWithdrawnRoutesDecodeFn() corebgp.DecodeFn[*updateMessage] {
	fn := corebgp.NewWithdrawnRoutesDecodeFn[*updateMessage](func(u *updateMessage, p []netip.Prefix) error {
		u.withdrawn = p
		return nil
	})
	apFn := corebgp.NewWithdrawnAddPathRoutesDecodeFn[*updateMessage](func(u *updateMessage, a []corebgp.AddPathPrefix) error {
		u.addPathWithdrawn = a
		return nil
	})
	return func(u *updateMessage, b []byte) error {
		if u.addPathIPv4 {
			return apFn(u, b)
		}
		return fn(u, b)
	}
}

func newNLRIDecodeFn() corebgp.DecodeFn[*updateMessage] {
	fn := corebgp.NewNLRIDecodeFn[*updateMessage](func(u *updateMessage, p []netip.Prefix) error {
		u.nlri = p
		return nil
	})
	apFn := corebgp.NewNLRIAddPathDecodeFn[*updateMessage](func(u *updateMessage, a []corebgp.AddPathPrefix) error {
		u.addPathNLRI = a
		return nil
	})
	return func(u *updateMessage, b []byte) error {
		if u.addPathIPv4 {
			return apFn(u, b)
		}
		return fn(u, b)
	}
}

func newPathAttrsDecodeFn() func(m *updateMessage, code uint8, flags corebgp.PathAttrFlags, b []byte) error {
	return func(m *updateMessage, code uint8, flags corebgp.PathAttrFlags, b []byte) error {
		switch code {
		case corebgp.PATH_ATTR_ORIGIN:
			var origin corebgp.OriginPathAttr
			err := origin.Decode(flags, b)
			if err != nil {
				return err
			}
			m.origin = uint8(origin)
			return nil
		case corebgp.PATH_ATTR_AS_PATH:
			var asPath corebgp.ASPathAttr
			err := asPath.Decode(flags, b)
			if err != nil {
				return err
			}
			m.asPath = asPath.ASSequence
			return nil
		case corebgp.PATH_ATTR_NEXT_HOP:
			var nextHop corebgp.NextHopPathAttr
			err := nextHop.Decode(flags, b)
			if err != nil {
				return err
			}
			m.nextHop = netip.Addr(nextHop)
			return nil
		case corebgp.PATH_ATTR_COMMUNITY:
			var comms corebgp.CommunitiesPathAttr
			err := comms.Decode(flags, b)
			if err != nil {
				return err
			}
			m.communities = comms
		}
		return nil
	}
}
