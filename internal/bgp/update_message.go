package bgp

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/juju/loggo"
	"github.com/jwhited/corebgp"
	"github.com/limhud/bgp2etcd/internal/messages"
	"github.com/palantir/stacktrace"
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

func (u updateMessage) IsEndOfRib() bool {
	return len(u.nlri) == 0 && len(u.addPathNLRI) == 0 && len(u.withdrawn) == 0 && len(u.addPathWithdrawn) == 0
}

func (u updateMessage) ToUpdateMessage() (*messages.UpdateMessage, error) {
	processedDeletions := make(map[netip.Prefix]bool)
	processedAdditions := make(map[netip.Prefix]bool)
	msg := messages.NewUpdateMessage()
	for _, prefix := range u.withdrawn {
		if processedDeletions[prefix] {
			continue
		}
		if err := msg.DeleteRoute(prefix, u.nextHop); err != nil {
			return nil, stacktrace.Propagate(err, "fail to add route deletion for prefix <%s> and next hop <%s>", prefix, u.nextHop)
		}
		processedDeletions[prefix] = true
	}
	for _, prefix := range u.nlri {
		if processedAdditions[prefix] {
			continue
		}
		if err := msg.AddRoute(prefix, u.nextHop); err != nil {
			return nil, stacktrace.Propagate(err, "fail to add route addition for prefix <%s> and next hop <%s>", prefix, u.nextHop)
		}
		processedAdditions[prefix] = true
	}
	for _, addPathPrefix := range u.addPathWithdrawn {
		if processedDeletions[addPathPrefix.Prefix] {
			continue
		}
		if err := msg.DeleteRoute(addPathPrefix.Prefix, u.nextHop); err != nil {
			return nil, stacktrace.Propagate(err, "fail to add route deletion for prefix <%s> and next hop <%s>", addPathPrefix.Prefix, u.nextHop)
		}
		processedDeletions[addPathPrefix.Prefix] = true
	}
	for _, addPathPrefix := range u.addPathNLRI {
		if processedAdditions[addPathPrefix.Prefix] {
			continue
		}
		if err := msg.AddRoute(addPathPrefix.Prefix, u.nextHop); err != nil {
			return nil, stacktrace.Propagate(err, "fail to add route addition for prefix <%s> and next hop <%s>", addPathPrefix.Prefix, u.nextHop)
		}
		processedAdditions[addPathPrefix.Prefix] = true
	}
	return msg, nil
}

func newWithdrawnRoutesDecodeFn() corebgp.DecodeFn[*updateMessage] {
	fn := corebgp.NewWithdrawnRoutesDecodeFn[*updateMessage](func(u *updateMessage, p []netip.Prefix) error {
		loggo.GetLogger("").Tracef("withdrawn route: %s", p)
		u.withdrawn = p
		return nil
	})
	apFn := corebgp.NewWithdrawnAddPathRoutesDecodeFn[*updateMessage](func(u *updateMessage, a []corebgp.AddPathPrefix) error {
		loggo.GetLogger("").Tracef("withdrawn add path route: %s", a)
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
		loggo.GetLogger("").Tracef("nlri route: %s", p)
		u.nlri = p
		return nil
	})
	apFn := corebgp.NewNLRIAddPathDecodeFn[*updateMessage](func(u *updateMessage, a []corebgp.AddPathPrefix) error {
		loggo.GetLogger("").Tracef("nlri add path route: %s", a)
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
		loggo.GetLogger("").Tracef("decoding attribute: code %d, flags %d, bytes <%v>", code, flags, b)
		switch code {
		case corebgp.PATH_ATTR_ORIGIN:
			var origin corebgp.OriginPathAttr
			err := origin.Decode(flags, b)
			if err != nil {
				loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to decode origin attribute").Error())
				return err
			}
			m.origin = uint8(origin)
			loggo.GetLogger("").Tracef("decoded origin attribute: %d", m.origin)
			return nil
		case corebgp.PATH_ATTR_AS_PATH:
			var asPath corebgp.ASPathAttr
			if len(b) == 0 {
				return nil
			}
			err := asPath.Decode(flags, b)
			if err != nil {
				loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to decode AS path attribute").Error())
				return err
			}
			m.asPath = asPath.ASSequence
			loggo.GetLogger("").Tracef("decoded AS path attribute: %#v", m.asPath)
			return nil
		case corebgp.PATH_ATTR_NEXT_HOP:
			var nextHop corebgp.NextHopPathAttr
			err := nextHop.Decode(flags, b)
			if err != nil {
				loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to decode next hop attribute").Error())
				return err
			}
			m.nextHop = netip.Addr(nextHop)
			loggo.GetLogger("").Tracef("decoded next hop attribute: %s", m.nextHop.String())
			return nil
		case corebgp.PATH_ATTR_COMMUNITY:
			var comms corebgp.CommunitiesPathAttr
			err := comms.Decode(flags, b)
			if err != nil {
				loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to decode community attribute").Error())
				return err
			}
			m.communities = comms
			loggo.GetLogger("").Tracef("decoded community attribute: %#v", m.communities)
			return nil
		}
		loggo.GetLogger("").Tracef("attribute ignored")
		return nil
	}
}
