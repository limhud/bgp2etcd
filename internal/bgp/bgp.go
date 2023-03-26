package bgp

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/juju/loggo"
	"github.com/jwhited/corebgp"
	"github.com/limhud/bgp2etcd/internal/config"
	"github.com/limhud/bgp2etcd/internal/messages"
	"github.com/limhud/bgp2etcd/internal/service"
	"github.com/palantir/stacktrace"
)

// Service manages the bgp server
type Service interface {
	service.I
}

type srv struct {
	service.Service
	updateChan chan *messages.UpdateMessage
	pluginChan chan *updateMessage
	bgpServer  *corebgp.Server
}

// NewService returns a new Service ready to be started.
func NewService(updateChan chan *messages.UpdateMessage) (Service, error) {
	if updateChan == nil {
		return nil, stacktrace.NewError("invalid <nil> updateChan")
	}
	s := &srv{updateChan: updateChan}
	if err := s.InitializeService("bgp service", s); err != nil {
		return nil, stacktrace.Propagate(err, "fail to initialize bgp service")
	}
	return s, nil
}

func (s *srv) Run(shutdownSignal chan time.Duration) error {
	var err error
	srvErrCh := make(chan error, 1)
ServiceLoop:
	for {
		// initialize bgp client with backoff
		initWait := time.Second
	InitializationLoop:
		for {
			s.bgpServer, err = s.getBgpServer()
			if err == nil {
				break InitializationLoop
			}
			loggo.GetLogger("").Warningf(stacktrace.Propagate(err, "bgp server initialization failure, try again").Error())
			select {
			case <-shutdownSignal:
				return nil
			case <-time.After(initWait):
			}
			if initWait < 5*time.Minute {
				initWait += 10 * time.Second
			}
		}
		initialFullRib := true
		initialFullMsg := messages.NewUpdateMessage()
		initialFullMsg.SetFullView()
		loggo.GetLogger("").Infof("bgp server initialized")
		// start bgp server
		go func() {
			srvErrCh <- s.bgpServer.Serve([]net.Listener{})
		}()
	ProcessLoop:
		for {
			select {
			case <-shutdownSignal:
				break ServiceLoop
			case err := <-srvErrCh:
				loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "bgp server crashed").Error())
				if s.bgpServer != nil {
					s.bgpServer.Close()
				}
				break ProcessLoop
			case bgpUpdate := <-s.pluginChan:
				if bgpUpdate == nil {
					loggo.GetLogger("").Warningf(stacktrace.NewError("received <nil> bgp update").Error())
					continue
				}
				if bgpUpdate.IsEndOfRib() {
					if !initialFullRib {
						// ignoring End-Of-Rib
						loggo.GetLogger("").Tracef("received unexpected End-of-Rib bgp update")
						continue ProcessLoop
					}
					loggo.GetLogger("").Tracef("sending initial full view message")
					select {
					case s.updateChan <- initialFullMsg:
					case <-shutdownSignal:
						break ServiceLoop
					}
					initialFullRib = false
					continue ProcessLoop
				}
				msg, err := bgpUpdate.ToUpdateMessage()
				if err != nil {
					loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to convert bgp update message <%s>", bgpUpdate).Error())
					continue ProcessLoop
				}
				if initialFullRib {
					loggo.GetLogger("").Tracef("merging message <%s> with initial full view message", msg)
					if err := initialFullMsg.Merge(msg); err != nil {
						loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to merge update message <%s> with initial full view message <%s>", msg, initialFullMsg).Error())
					}
					continue ProcessLoop
				}
				loggo.GetLogger("").Tracef("sending message")
				select {
				case s.updateChan <- msg:
				case <-shutdownSignal:
					break ServiceLoop
				}
			}
		}
	}
	return nil
}

func (s *srv) getBgpServer() (*corebgp.Server, error) {
	// initialize bgp server
	corebgp.SetLogger(func(v ...interface{}) {
		var strBuilder strings.Builder
		fmt.Fprint(&strBuilder, v)
		loggo.GetLogger("").Tracef(strBuilder.String())
	})
	bgpServer, err := corebgp.NewServer(config.GetBgp().RouterID)
	if err != nil {
		return nil, stacktrace.Propagate(err, "fail to initialize bgp server")
	}
	s.pluginChan = make(chan *updateMessage, 1000)
	bgpPlugin, err := NewPlugin(s.pluginChan)
	if err != nil {
		return nil, stacktrace.Propagate(err, "fail to initialize bgp plugin")
	}
	peerOpts := []corebgp.PeerOption{
		corebgp.WithLocalAddress(config.GetBgp().LocalAddress),
		corebgp.WithPort(config.GetBgp().PeerPort),
	}
	peerConfig := corebgp.PeerConfig{
		RemoteAddress: config.GetBgp().PeerAddress,
		LocalAS:       config.GetBgp().LocalAS,
		RemoteAS:      config.GetBgp().PeerAS,
	}
	err = bgpServer.AddPeer(peerConfig, bgpPlugin, peerOpts...)
	if err != nil {
		return nil, stacktrace.Propagate(err, "fail to add peer to bgp server")
	}
	return bgpServer, nil
}

func (s *srv) Release() error {
	if s.bgpServer != nil {
		s.bgpServer.Close()
	}
	return nil
}
