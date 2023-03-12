package service

import (
	"sync"
	"time"

	"github.com/juju/loggo"
	"github.com/palantir/stacktrace"

	"github.com/limhud/bgp2etcd/internal/errorcode"
)

// State is the type for the different service states.
type State uint

const (
	// States needs to be ordered according to the lifecycle of the service. This allows tests like GetState() > StateRunning.

	// StateUninitialized is 0 since it is the default value for the state field.
	StateUninitialized State = iota
	// StateInitialized : InitializeService has been sucessfuly called.
	StateInitialized
	// StateStarting : Start has been called but the service is not yet running.
	StateStarting
	// StateRunning : the service is currently running
	StateRunning
	// StateStopping : Shutdown has been called but the service is not yet stopped.
	StateStopping
	// StateReleased : Shutdown has been called and Release has been run.
	StateReleased
	// StateStopped : the service is completely shutdown.
	StateStopped
)

func (s State) String() string {
	return [...]string{"uninitialized", "initialized", "starting", "running", "stopping", "released", "stopped"}[s]
}

// I represents the Service behaviour.
type I interface {
	Start() error
	Done() chan error
	Shutdown(graceful time.Duration, hard time.Duration) error
	GetServiceName() string
	GetState() State
}

// Service is a struct implementing a lifecycle around a goroutine. It provides the following methods:
// * Start
// * Done
// * Shutdown
// * GetState
// It needs to be composited with an other struct implementing the specific behaviour of the service represented by the SubService interface.
// At instanciation, the composited structure needs to call the InitializeService method with the required parameters.
type Service struct {
	mutex          sync.RWMutex
	done           chan error
	doneChanList   []chan error
	state          State
	name           string
	subservice     SubService
	shutdownSignal chan time.Duration
	shutdownErr    error
}

// SubService must be implemented by the composited struct.
// The Run method can be a simple wait on shutdownSignal chan if there is no need for an active goroutine in the service.
type SubService interface {
	Initialize() error
	Run(shutdownSignal chan time.Duration) error // The shutdownSignal is a time.Duration that can be used to emulate a graceful shutdown.
	Release() error
}

// Initialize implements SubService.Initialize with a noop function.
// It relieves the composited structure from implementing it when it is not useful.
func (s *Service) Initialize() error {
	return nil
}

// Run implements SubService.Run with a noop function.
// It relieves the composited structure from implementing it when it is not useful.
func (s *Service) Run(shutdownSignal chan time.Duration) error {
	<-shutdownSignal
	return nil
}

// Release implements SubService.Release with a noop function.
// It relieves the composited structure from implementing it when it is not useful.
func (s *Service) Release() error {
	return nil
}

// InitializeService needs to be called after the instanciation of the composited struct in order to initialize the service internals.
// The required parameters are a name for more explicit logs and the composited struct itself.
func (s *Service) InitializeService(name string, sub SubService) error {
	if sub == nil {
		return stacktrace.NewError("invalid <nil> sub service")
	}
	if len(name) == 0 {
		return stacktrace.NewError("invalid empty service name")
	}
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.state != StateUninitialized {
		return stacktrace.NewError("Initialized called on already initialized service")
	}
	s.name = name
	s.subservice = sub
	s.done = make(chan error, 1)
	s.shutdownSignal = make(chan time.Duration, 1)
	s.state = StateInitialized
	return nil
}

// Start starts the service.
// First, it calls the Initialize method of the composited struct
// Then, it runs the goroutine in charge of calling the Run and the Release methods of the composited struct.
func (s *Service) Start() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	loggo.GetLogger("").Infof("[%s] starting service", s.name)
	if s.state != StateInitialized {
		return stacktrace.NewError("refusing to start if not in initialized state")
	}
	s.state = StateStarting
	if err := s.subservice.Initialize(); err != nil {
		return stacktrace.Propagate(err, "fail to start service")
	}
	// run the goroutine in charge of calling the Run method
	go func(name string, subservice SubService, shutdownSignal chan time.Duration, done chan error) {
		s.mutex.Lock()
		s.state = StateRunning
		s.mutex.Unlock()
		loggo.GetLogger("").Infof("[%s] service started", name)
		runErr := subservice.Run(shutdownSignal)
		if runErr != nil {
			loggo.GetLogger("").Errorf("[%s] service stopping after error: %s", name, runErr)
		} else {
			loggo.GetLogger("").Infof("[%s] service stopping", name)
		}
		releaseErr := subservice.Release()
		if releaseErr != nil {
			loggo.GetLogger("").Errorf("[%s] service released with error: %s", name, releaseErr)
		} else {
			loggo.GetLogger("").Infof("[%s] service released", name)
		}
		s.mutex.Lock()
		s.state = StateReleased
		s.mutex.Unlock()
		if runErr != nil {
			done <- runErr
		} else {
			done <- releaseErr
		}
		close(done)
	}(s.name, s.subservice, s.shutdownSignal, s.done)
	// run the goroutine in charge of closing the doneChan opened for the callers of Done() and Shutdown() methods.
	go func(name string, done chan error) {
		err := <-done
		s.mutex.Lock()
		defer s.mutex.Unlock()
		s.shutdownErr = err
		for _, doneChan := range s.doneChanList {
			func() { // a function is used to leverage the defer/recover mechanism for each doneChan in case a chan is closed and triggers a panic
				defer func() {
					if r := recover(); r != nil {
						loggo.GetLogger("").Errorf("[%s] fail to propagate error to done channel: %s", name, r)
					}
				}()
				if err != nil {
					select {
					case doneChan <- err:
					default:
						loggo.GetLogger("").Errorf("[%s] fail to propagate error to done channel", name)
					}
				}
				close(doneChan)
			}()
		}
		s.state = StateStopped
		loggo.GetLogger("").Infof("[%s] service stopped", name)
	}(s.name, s.done)
	return nil
}

// Done returns a channel that will be closed at service shutdown.
// If the service failed at runtime or during shutdown, the channel will contain an error.
func (s *Service) Done() chan error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	done := make(chan error, 1)
	if s.state == StateStopped {
		if s.shutdownErr != nil {
			done <- s.shutdownErr
		}
		close(done)
		return done
	}
	s.doneChanList = append(s.doneChanList, done)
	return done
}

// Shutdown send the signal to stop the service and wait for it to stop.
// The error returned is the same as the one retrieved in Done().
// The graceful timeout is given to the Run method of the subservice.
// The hard timeout is only used in this call of Shutdown in order to specify how much time to wait for shutdown. 0 means wait forever.
func (s *Service) Shutdown(graceful time.Duration, hard time.Duration) error {
	if graceful < 0 {
		return stacktrace.NewError("invalid graceful timeout <%s>", graceful)
	}
	if hard < 0 {
		return stacktrace.NewError("invalid hard timeout <%s>", hard)
	}
	s.mutex.Lock()
	loggo.GetLogger("").Tracef("[%s] received shutdown with graceful timeout <%s> and hard timeout <%s>", s.name, graceful, hard)
	switch s.state {
	case StateStarting, StateRunning:
		loggo.GetLogger("").Tracef("[%s] transmitting shutdown with graceful timeout <%s> and hard timeout <%s>", s.name, graceful, hard)
		s.state = StateStopping
		s.mutex.Unlock()
		s.shutdownSignal <- graceful
		close(s.shutdownSignal)
	case StateStopping, StateReleased, StateStopped:
		s.mutex.Unlock()
	default:
		defer s.mutex.Unlock()
		return stacktrace.NewErrorWithCode(errorcode.EcodeServiceNotStarted, "cannot shutdown a service in state <%s>", s.state)
	}
	if hard > 0 {
		select {
		case err := <-s.Done():
			return err
		case <-time.After(hard):
			return stacktrace.NewErrorWithCode(errorcode.EcodeServiceTimeout, "timeout expired after <%s>", hard)
		}
	}
	return <-s.Done()
}

// GetServiceName returns the service name
func (s *Service) GetServiceName() string {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.name
}

// GetState returns the current state of the service.
func (s *Service) GetState() State {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.state
}
