package etcd

import (
	"time"

	"github.com/juju/loggo"
	"github.com/palantir/stacktrace"
	"go.etcd.io/etcd/clientv3"

	"github.com/limhud/bgp2etcd/internal/config"
	"github.com/limhud/bgp2etcd/internal/service"
)

// Service handles etcd updates
type Service interface {
	service.I
}

type srv struct {
	service.Service
	etcdClient *clientv3.Client
	updateChan chan *UpdateMessage
}

// NewService returns a new Service ready to be started.
func NewService(updateChan chan *UpdateMessage) (Service, error) {
	if updateChan == nil {
		return nil, stacktrace.NewError("invalid <nil> updateChan")
	}
	s := &srv{updateChan: updateChan}
	if err := s.InitializeService("etcd service", s); err != nil {
		return nil, stacktrace.Propagate(err, "fail to initialize etcd service")
	}
	return s, nil
}

func (s *srv) Run(shutdownSignal chan time.Duration) error {
	var err error
	for {
		// initialize etcd client with backoff
		initWait := time.Second
	InitializationLoop:
		for {
			s.etcdClient, err = s.getEtcdClient()
			if err == nil {
				break InitializationLoop
			}
			loggo.getLogger("").Warningf(stacktrace.Propagate(err, "etcd client initialization failure, try again").Error())
			select {
			case <-shutdownSignal:
				return nil
			case <-time.After(initWait):
			}
			if initWait < 5*time.Minute {
				initWait += 10 * time.Second
			}
		}
		loggo.GetLogger("").Infof("etcd client initialized")
		// processing loop
	ProcessingLoop:
		for {
			select {
			case <-shutdownSignal:
				return nil
			case msg := <-s.updateChan:
				// TODO
			}
		}
	}
	return nil
}

func (s *srv) getEtcdClient() (*clientv3.Client, error) {
	// initialize etcd client
	etcdConfig := clientv3.Config{
		Endpoints:   config.GetEtcd().Endpoints,
		DialTimeout: config.GetEtcd().DialTimeout,
		Username:    config.GetEtcd().Username,
		Password:    config.GetEtcd().Password,
		Context:     ctx,
	}
	loggo.GetLogger("").Debugf("etcd config: <%#v>", etcdConfig)
	etcdClient, err = clientv3.New(etcdConfig)
	if err != nil {
		return nil, stacktrace.Propagate(err, "fail to initialize etcd client with config <%#v>", etcdConfig)
	}
	return etcdClient, nil
}

func (s *srv) Release() error {
	if s.etcdClient != nil && s.etcdClient.ActiveConnection() != nil {
		err = s.etcdClient.Close()
		if err != nil {
			loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to close etcd client").Error())
		}
	}
	return nil
}
