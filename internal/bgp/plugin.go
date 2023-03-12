package bgp

import (
	"net/netip"

	"github.com/juju/loggo"
	"github.com/jwhited/corebgp"
	"github.com/palantir/stacktrace"
)

// plugin handles BGP messages
type plugin struct {
	updateChan  chan *UpdateMessage
	decoder     *corebgp.UpdateDecoder[*updateMessage]
	addPathIPv4 bool
}

func NewPlugin(updateChan chan *UpdateMessage) (corebgp.Plugin, error) {
	if updateChan == nil {
		return nil, stacktrace.NewError("invalid <nil> updateChan")
	}
	decoder := corebgp.NewUpdateDecoder[*updateMessage](
		newWithdrawnRoutesDecodeFn(),
		newPathAttrsDecodeFn,
		newNLRIDecodeFn(),
	)
	p := &plugin{
		updateChan: updateChan,
		decoder:    decoder,
	}
	return p, nil
}

func (p *plugin) GetCapabilities(peer corebgp.PeerConfig) []corebgp.Capability {
	caps := make([]corebgp.Capability, 0)
	caps = append(caps, corebgp.NewMPExtensionsCapability(corebgp.AFI_IPV4, corebgp.SAFI_UNICAST))
	tuples := make([]corebgp.AddPathTuple, 0)
	tuples = append(tuples, corebgp.AddPathTuple{
		AFI:  corebgp.AFI_IPV4,
		SAFI: corebgp.SAFI_UNICAST,
		Tx:   true,
		Rx:   true,
	})
	caps = append(caps, corebgp.NewAddPathCapability(tuples))
	return caps
}

func (p *plugin) OnOpenMessage(peer corebgp.PeerConfig, routerID netip.Addr, capabilities []corebgp.Capability) *corebgp.Notification {
	p.addPathIPv4 = false
	for _, c := range capabilities {
		if c.Code != corebgp.CAP_ADD_PATH {
			continue
		}
		tuples, err := corebgp.DecodeAddPathTuples(c.Value)
		if err != nil {
			return err.(*corebgp.Notification)
		}
		for _, tuple := range tuples {
			if tuple.SAFI != corebgp.SAFI_UNICAST || !tuple.Tx {
				continue
			}
			if tuple.AFI == corebgp.AFI_IPV4 {
				p.addPathIPv4 = true
			}
		}
	}
	return nil
}

func (p *plugin) OnEstablished(peer corebgp.PeerConfig, writer corebgp.UpdateMessageWriter) corebgp.UpdateMessageHandler {
	// send End-of-Rib
	writer.WriteUpdate([]byte{0, 0, 0, 0})
	return p.handleUpdate
}

func (p *plugin) OnClose(peer corebgp.PeerConfig) {
}

func (p *plugin) handleUpdate(peer corebgp.PeerConfig, b []byte) *corebgp.Notification {
	msg := &updateMessage{addPathIPv4: p.addPathIPv4}
	if err := p.decoder.Decode(msg, b); err != nil {
		return corebgp.UpdateNotificationFromErr(err)
	}
	loggo.GetLogger("").Debugf("received bgp update: %s", msg)
	// TODO: process message and send to updateChan
	return nil
}
