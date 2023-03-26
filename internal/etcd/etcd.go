package etcd

import (
	"context"
	"fmt"
	"time"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/clientv3/concurrency"
	"github.com/juju/loggo"
	"github.com/palantir/stacktrace"

	"github.com/limhud/bgp2etcd/internal/config"
	"github.com/limhud/bgp2etcd/internal/messages"
	"github.com/limhud/bgp2etcd/internal/service"
)

// Service handles etcd updates
type Service interface {
	service.I
}

type srv struct {
	service.Service
	etcdClient  *clientv3.Client
	etcdSession *concurrency.Session
	updateChan  chan *messages.UpdateMessage
}

// NewService returns a new Service ready to be started.
func NewService(updateChan chan *messages.UpdateMessage) (Service, error) {
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
	lockPrefix := config.GetEtcd().LockPrefix
	msgBuffer := []*messages.UpdateMessage{}
	wakeUpChan := make(chan struct{}, 1)
	shutdown := false
RunLoop:
	for {
		// initialize etcd client with backoff
		initWait := time.Second
	InitializationLoop:
		for {
			s.etcdClient, s.etcdSession, err = s.getEtcdClient()
			if err == nil {
				break InitializationLoop
			}
			loggo.GetLogger("").Warningf(stacktrace.Propagate(err, "etcd client initialization failure, try again").Error())
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
		globalMutex := concurrency.NewMutex(s.etcdSession, fmt.Sprintf("%s/global", lockPrefix))
	ProcessingLoop:
		for {
			select {
			case <-shutdownSignal:
				shutdown = true
				break ProcessingLoop
			case msg := <-s.updateChan:
				loggo.GetLogger("").Tracef("received messages for etcd persistance: <%s>", msg)
				msgBuffer = append(msgBuffer, msg)
				select {
				case wakeUpChan <- struct{}{}:
				default:
					loggo.GetLogger("").Tracef("wakeUpChan is already filled, no need to add a new trigger")
				}
			case <-wakeUpChan:
				if len(msgBuffer) == 0 {
					continue ProcessingLoop
				}
				msg := msgBuffer[0]
				ctx, cancel := context.WithCancel(context.Background())
				errChan := make(chan error, 1)
				go func() {
					errChan <- s.store(ctx, msg, globalMutex)
				}()
				select {
				case err := <-errChan:
					if err != nil {
						loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to store update <%s>", msg.String()).Error())
						wakeUpChan <- struct{}{}
						break ProcessingLoop
					}
					loggo.GetLogger("").Tracef("message successfully persisted: <%s>", msg)
				case <-shutdownSignal:
					cancel()
					<-errChan // waiting for go routine to cancel
					shutdown = true
					break ProcessingLoop
				}
				msgBuffer = msgBuffer[1:]
				if len(msgBuffer) > 0 {
					wakeUpChan <- struct{}{}
				}
			}
		}
		// release session and client
		if s.etcdSession != nil {
			if err := s.etcdSession.Close(); err != nil {
				loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to close etcd session").Error())
			}
		}
		if s.etcdClient != nil && s.etcdClient.ActiveConnection() != nil {
			if err := s.etcdClient.Close(); err != nil {
				loggo.GetLogger("").Errorf(stacktrace.Propagate(err, "fail to close etcd client").Error())
			}
		}
		if shutdown {
			break RunLoop
		}
	}
	return nil
}

func (s *srv) store(ctx context.Context, msg *messages.UpdateMessage, globalMutex *concurrency.Mutex) error {
	if msg == nil {
		return stacktrace.NewError("invalid <nil> messages")
	}
	if globalMutex == nil {
		return stacktrace.NewError("invalid <nil> globalMutex")
	}
	pathPrefix := config.GetEtcd().Prefix
	if err := globalMutex.Lock(ctx); err != nil {
		return stacktrace.Propagate(err, "fail to acquire globalMutex")
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := globalMutex.Unlock(unlockCtx); err != nil {
			loggo.GetLogger("").Warningf(stacktrace.Propagate(err, "fail to release global lock").Error())
		}
	}()
	cache, err := NewCache(pathPrefix, s.etcdClient)
	if err != nil {
		return stacktrace.Propagate(err, "fail to obtain a new cache structure")
	}
	if msg.IsFullView() {
		if err := cache.RemoveAll(); err != nil {
			return stacktrace.Propagate(err, "fail to remove all keys")
		}
		for nextHop, prefixes := range msg.GetAdditions() {
			for _, prefix := range prefixes {
				if err := cache.Add(ctx, nextHop, prefix); err != nil {
					return stacktrace.Propagate(err, "fail to add prefix <%s> to current nextHop <%s>", prefix.String(), nextHop.String())
				}
			}
		}
	} else {
		for nextHop, prefixes := range msg.GetDeletions() {
			for _, prefix := range prefixes {
				if err := cache.Remove(ctx, nextHop, prefix); err != nil {
					return stacktrace.Propagate(err, "fail to remove prefix <%s> from current nextHop <%s>", prefix.String(), nextHop.String())
				}
			}
		}
		for nextHop, prefixes := range msg.GetAdditions() {
			for _, prefix := range prefixes {
				if err := cache.Add(ctx, nextHop, prefix); err != nil {
					return stacktrace.Propagate(err, "fail to add prefix <%s> to current nextHop <%s>", prefix.String(), nextHop.String())
				}
			}
		}
	}
	operations, err := cache.GetOperations()
	if err != nil {
		return stacktrace.Propagate(err, "fail to obtain operation list")
	}
	for _, opList := range operations {
		if _, err := s.etcdClient.Txn(ctx).Then(opList...).Commit(); err != nil {
			return stacktrace.Propagate(err, "transaction failed for operations <%#v>", opList)
		}
	}
	return nil
}

func (s *srv) getEtcdClient() (*clientv3.Client, *concurrency.Session, error) {
	// initialize etcd client
	etcdConfig := clientv3.Config{
		Endpoints:   config.GetEtcd().Endpoints,
		DialTimeout: config.GetEtcd().DialTimeout,
		Username:    config.GetEtcd().Username,
		Password:    config.GetEtcd().Password,
		//Context:     ctx,
	}
	loggo.GetLogger("").Debugf("etcd config: <%#v>", etcdConfig)
	etcdClient, err := clientv3.New(etcdConfig)
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "fail to initialize etcd client with config <%#v>", etcdConfig)
	}
	session, err := concurrency.NewSession(etcdClient)
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "fail to initialize etcd session")
	}
	return etcdClient, session, nil
}
