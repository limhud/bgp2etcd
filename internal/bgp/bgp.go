package bgp

import (
	"net"
	"time"

	"github.com/juju/loggo"
	"github.com/jwhited/corebgp"
	"github.com/limhud/bgp2etcd/internal/config"
	"github.com/palantir/stacktrace"
)

// Service manages the bgp server
type Service interface {
	service.I
}

type srv struct {
	service.Service
	updateChan chan *UpdateMessage
	bgpServer  *corebgp.Server
}

// NewService returns a new Service ready to be started.
func NewService(updateChan chan *UpdateMessage) (Service, error) {
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
	var listener net.Listener
	srvErrCh := make(chan error, 1)
ServiceLoop:
	for {
		// initialize bgp client with backoff
		initWait := time.Second
	InitializationLoop:
		for {
			s.bgpServer, listener, err = s.getBgpServer()
			if err == nil {
				break InitializationLoop
			}
			loggo.getLogger("").Warningf(stacktrace.Propagate(err, "bgp server initialization failure, try again").Error())
			select {
			case <-shutdownSignal:
				return nil
			case <-time.After(initWait):
			}
			if initWait < 5*time.Minute {
				initWait += 10 * time.Second
			}
		}
		loggo.GetLogger("").Infof("bgp server initialized")
		// start bgp server
		go func(listener net.Listener) {
			err := service.BgpServer.Serve([]net.Listener{listener})
			srvErrCh <- err
		}(listener)
		select {
		case <-shutdownSignal:
			break ServiceLoop
		case err := <-srvErrCh:
			loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "bgp server crashed").Error())
			if s.bgpServer != nil {
				s.bgpServer.Close()
			}
		}
	}
	return nil
}

func (s *srv) getBgpServer() (*corebgp.Server, net.Listener, error) {
	// initialize bgp server
	corebgp.SetLogger(loggo.GetLogger("").Tracef)
	bgpServer, err := corebgp.NewServer(config.GetBgp().RouterID)
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "fail to initialize bgp server")
	}
	bgpPlugin, err := NewPlugin(s.updateChan)
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "fail to initialize bgp plugin")
	}
	peerOpts := make([]corebgp.PeerOption, 0)
	peerOpts := append(peerOpts, corebgp.WithPort(config.GetBgp().PeerPort))
	err = service.BgpServer.AddPeer(corebgp.PeerConfig{
		RemoteAddress: config.GetBgp().PeerAddress,
		LocalAS:       config.GetBgp().LocalAS,
		RemoteAS:      config.GetBgp().RemoteAS,
	}, bgpPlugin, peerOpts...)
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "fail to add peer to bgp server")
	}
	listener, err := net.Listen("tcp", config.GetBgp().LocalAddress.String())
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "fail to setup bgp listener on <%s>", config.GetBgp().LocalAddress)
	}
	return bgpServer, listener, nil
}

func (s *srv) Release() error {
	if s.bgpServer != nil {
		s.bgpServer.Close()
	}
	return nil
}
