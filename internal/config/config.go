/*
Package config implements a thread safe configuration yaml file parser.
*/
package config

import (
	"fmt"
	"net/netip"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/juju/loggo"
	"github.com/palantir/stacktrace"
	"gopkg.in/yaml.v2"
)

var (
	lock              sync.Mutex
	configFilePath    string
	immutableLogLevel bool
	// Configuration is the global Config instance storing the current configuration
	Configuration Config
)

// --- BgpConfig section

// BgpConfig represents the unix socket configuration
type BgpConfig struct {
	LocalAS      uint32     `yaml:"local_as"`
	LocalAddress netip.Addr `yaml:"local_address"`
	PeerAS       uint32     `yaml:"peer_as"`
	PeerAddress  netip.Addr `yaml:"peer_address"`
	PeerPort     int        `yaml:"peer_port"`
	RouterID     netip.Addr `yaml:"router_id"`
}

func (bgp *BgpConfig) validate() error {
	if bgp.LocalAS == 0 {
		return stacktrace.NewError("<local_as> field is required and should not be 0")
	}
	if bgp.LocalAddress == nil {
		return stacktrace.NewError("<local_address> field is required")
	}
	if bgp.PeerAS == 0 {
		return stacktrace.NewError("<peer_as> field is required and should not be 0")
	}
	if bgp.PeerAddress == nil {
		return stacktrace.NewError("<peer_address> field is required")
	}
	if bgp.PeerPort < 1 {
		return stacktrace.NewError("<peer_port> field is required and should be >= 1")
	}
	if bgp.RouterID == nil {
		return stacktrace.NewError("<router_id> field is required")
	}
	return nil
}

// Equal tests if content is the same
func (bgp *BgpConfig) Equal(comparedWith *BgpConfig) error {
	if comparedWith == nil {
		return stacktrace.NewError("cannot compare with <nil>")
	}
	if bgp.LocalAS != comparedWith.LocalAS {
		return stacktrace.NewError("LocalAS value <%s> is different: <%s>", bgp.LocalAS, comparedWith.LocalAS)
	}
	if bgp.LocalAddress != comparedWith.LocalAddress {
		return stacktrace.NewError("LocalAddress value <%s> is different: <%s>", bgp.LocalAddress, comparedWith.LocalAddress)
	}
	if bgp.PeerAS != comparedWith.PeerAS {
		return stacktrace.NewError("PeerAS value <%s> is different: <%s>", bgp.PeerAS, comparedWith.PeerAS)
	}
	if bgp.PeerAddress != comparedWith.PeerAddress {
		return stacktrace.NewError("PeerAddress value <%s> is different: <%s>", bgp.PeerAddress, comparedWith.PeerAddress)
	}
	if bgp.PeerPort != comparedWith.PeerPort {
		return stacktrace.NewError("PeerPort value <%s> is different: <%s>", bgp.PeerPort, comparedWith.PeerPort)
	}
	if bgp.RouterID != comparedWith.RouterID {
		return stacktrace.NewError("RouterID value <%s> is different: <%s>", bgp.RouterID, comparedWith.RouterID)
	}
	return nil
}

// Copy returns a copy of the object
func (bgp *BgpConfig) Copy() *BgpConfig {
	return &BgpConfig{
		LocalAS:      bgp.LocalAS,
		LocalAddress: bgp.LocalAddress,
		PeerAS:       bgp.PeerAS,
		PeerAddress:  bgp.PeerAddress,
		PeerPort:     bgp.PeerPort,
		RouterID:     bgp.RouterID,
	}
}

// --- EtcdConfig section

// EtcdConfig stores different parameters used for administrating the SFTP accounts
type EtcdConfig struct {
	Endpoints   []string      `yaml:"endpoints"`
	DialTimeout time.Duration `yaml:"dial_timeout"`
	Username    string        `yaml:"username"`
	Password    string        `yaml:"password"`
	Prefix      string        `yaml:"prefix"`
}

func (etcd *EtcdConfig) validate() error {
	if len(etcd.Endpoints) < 1 {
		return stacktrace.NewError("<endpoints> field is required")
	}
	if etcd.DialTimeout == 0 {
		return stacktrace.NewError("<dial_timeout> field is required and cannot be <0>")
	}
	if etcd.Prefix == "" {
		return stacktrace.NewError("<prefix> field is required")
	}
	return nil
}

// Equal tests if content is the same
func (etcd *EtcdConfig) Equal(comparedWith *EtcdConfig) error {
	if comparedWith == nil {
		return stacktrace.NewError("cannot compare with <nil>")
	}
	if !reflect.DeepEqual(etcd.Endpoints, comparedWith.Endpoints) {
		return stacktrace.NewError("Endpoints value <%s> is different: <%s>", etcd.Endpoints, comparedWith.Endpoints)
	}
	if etcd.DialTimeout != comparedWith.DialTimeout {
		return stacktrace.NewError("DialTimeout value <%s> is different: <%s>", etcd.DialTimeout, comparedWith.DialTimeout)
	}
	if etcd.Username != comparedWith.Username {
		return stacktrace.NewError("Username value <%s> is different: <%s>", etcd.Username, comparedWith.Username)
	}
	if etcd.Password != comparedWith.Password {
		return stacktrace.NewError("Password value <%s> is different: <%s>", etcd.Password, comparedWith.Password)
	}
	if etcd.Prefix != comparedWith.Prefix {
		return stacktrace.NewError("Prefix value <%s> is different: <%s>", etcd.Prefix, comparedWith.Prefix)
	}
	return nil
}

// Copy returns a copy of the object
func (etcd *EtcdConfig) Copy() *EtcdConfig {
	return &EtcdConfig{
		Endpoints:   etcd.Endpoints,
		DialTimeout: etcd.DialTimeout,
		Username:    etcd.Username,
		Password:    etcd.Password,
		Prefix:      etcd.Prefix,
	}
}

// --- Global Config section

// Config file structure definition
type Config struct {
	Debug bool       `yaml:"debug"`
	Bgp   BgpConfig  `yaml:"bgp"`
	Etcd  EtcdConfig `yaml:"etcd"`
}

func (c *Config) validate() error {
	var (
		err error
	)
	err = c.Bgp.validate()
	if err != nil {
		return stacktrace.Propagate(err, "fail to validate <bgp> section")
	}
	err = c.Etcd.validate()
	if err != nil {
		return stacktrace.Propagate(err, "fail to validate <etcd> section")
	}
	return nil
}

// String returns a string representing a config struct.
func (c *Config) String() string {
	return fmt.Sprintf("%#v", c)
}

// Equal tests if content is the same
func (c *Config) Equal(comparedWith *Config) error {
	var (
		err error
	)
	if comparedWith == nil {
		return stacktrace.NewError("cannot compare with <%s>", comparedWith)
	}
	if c.Debug != comparedWith.Debug {
		return stacktrace.NewError("debug value <%t> is different: <%t>", c.Debug, comparedWith.Debug)
	}
	err = c.Bgp.Equal(&comparedWith.Bgp)
	if err != nil {
		return stacktrace.Propagate(err, "bgp section is different")
	}
	err = c.Etcd.Equal(&comparedWith.Etcd)
	if err != nil {
		return stacktrace.Propagate(err, "etcd section is different")
	}
	return nil
}

// SetConfigFile set the path to the config file to read.
func SetConfigFile(path string) {
	configFilePath = path
}

// ReadInConfig triggers the reading of the config from the file.
func ReadInConfig() error {
	var (
		err       error
		data      []byte
		tmpConfig Config
	)

	lock.Lock()
	defer lock.Unlock()

	//read file and unmarshal yaml
	data, err = os.ReadFile(configFilePath)
	if err != nil {
		return stacktrace.Propagate(err, "fail to read <%s>", configFilePath)
	}

	loggo.GetLogger("").Debugf("config file <%s> read successfully", configFilePath)
	err = yaml.Unmarshal(data, &tmpConfig)
	if err != nil {
		return stacktrace.Propagate(err, "parsing error in <%s>", configFilePath)
	}
	loggo.GetLogger("").Debugf("config file <%s> parsed successfully", configFilePath)
	err = tmpConfig.validate()
	if err != nil {
		return stacktrace.Propagate(err, "fail to validate <%s>", configFilePath)
	}

	Configuration = tmpConfig

	if !immutableLogLevel {
		if Configuration.Debug {
			loggo.GetLogger("").SetLogLevel(loggo.DEBUG)
		} else {
			loggo.GetLogger("").SetLogLevel(loggo.INFO)
		}
	}

	loggo.GetLogger("").Debugf("config struct: <%#v>", Configuration)

	return err
}

// String returns a string representing the config object.
func String() (string, error) {
	var (
		err  error
		data []byte
	)

	lock.Lock()
	defer lock.Unlock()

	data, err = yaml.Marshal(Configuration)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("config file <%s>:\n%s", configFilePath, data), err
}

// GetBgp returns the bgp config section
func GetBgp() *BgpConfig {
	lock.Lock()
	defer lock.Unlock()

	return Configuration.Bgp.Copy()
}

// GetEtcd returns the etcd config section
func GetEtcd() *EtcdConfig {
	lock.Lock()
	defer lock.Unlock()

	return Configuration.Etcd.Copy()
}

// GetDebug returns true if debug is activated in the config file, false otherwise.
func GetDebug() bool {
	lock.Lock()
	defer lock.Unlock()

	return Configuration.Debug
}

// SetLogLevelImmutable sets a flag to deactivate log level modification by configuration
func SetLogLevelImmutable() {
	lock.Lock()
	defer lock.Unlock()

	immutableLogLevel = true
}
