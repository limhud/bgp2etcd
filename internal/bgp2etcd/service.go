package bgp2etcd

import (
	"time"

	"github.com/juju/loggo"
	"github.com/limhud/bgp2etcd/internal/bgp"
	"github.com/limhud/bgp2etcd/internal/etcd"
	"github.com/limhud/bgp2etcd/internal/service"
	"github.com/palantir/stacktrace"
)

// Service represents a service struct.
type Service interface {
	service.I
	ToggleDebug()
}

type srv struct {
	service.Service
	bgpService  bgp.Service
	etcdService etcd.Service
	updateChan  chan *UpdateMessage
}

// NewService returns a new service instance.
func NewService() (Service, error) {
	updateChan := make(chan *UpdateMessage, 1000)
	bgpService, err := bgp.NewService(updateChan)
	if err != nil {
		return nil, stacktrace.Propagate(err, "fail to create bgp service")
	}
	etcdService, err := etcd.NewService(updateChan)
	if err != nil {
		return nil, stacktrace.Propagate(err, "fail to create etcd service")
	}
	s := &srv{
		bgpService:  bgpService,
		etcdService: etcdService,
		updateChan:  updateChan,
	}
	if err := s.InitializeService("bgp2etcd service", s); err != nil {
		return nil, stacktrace.Propagate(err, "fail to initialize bgp2etcd service")
	}
	return s, nil
}

// ToggleDebug toggles log levele between DEBUG and INFO.
func (service *Service) ToggleDebug() {
	if loggo.GetLogger("").LogLevel() == loggo.INFO {
		loggo.GetLogger("").Infof("setting log level to Debug")
		loggo.GetLogger("").SetLogLevel(loggo.DEBUG)
	} else if loggo.GetLogger("").LogLevel() == loggo.DEBUG {
		loggo.GetLogger("").Infof("setting log level to Trace")
		loggo.GetLogger("").SetLogLevel(loggo.TRACE)
	} else {
		loggo.GetLogger("").Infof("setting log level to Info")
		loggo.GetLogger("").SetLogLevel(loggo.INFO)
	}
}

// Start the etc and bgp services.
func (s *srv) Run(shutdownSignal chan time.Duration) error {
	if err := s.etcdService.Start(); err != nil {
		return stacktrace.Propagate(err, "fail to start etcd service")
	}
	if err := s.bgpService.Start(); err != nil {
		return stacktrace.Propagate(err, "fail to start bgp service")
	}
	<-shutdownSignal
	return nil
}

// Release stop the etcd and bgp services
func (s *srv) Release() error {
	if s.bgpService != nil {
		if err := s.bgpService.Shutdown(0, 15*time.Second); err != nil {
			loggo.GetLogger("").Errorf(err.Error())
		}
	}
	if s.etcdService != nil {
		if err := s.etcdService.Shutdown(0, 15*time.Second); err != nil {
			loggo.GetLogger("").Errorf(err.Error())
		}
	}
	return nil
}
