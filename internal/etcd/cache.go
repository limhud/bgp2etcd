package etcd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"

	"github.com/coreos/etcd/clientv3"
	"github.com/palantir/stacktrace"
)

type cache struct {
	pathPrefix string
	etcdClient *clientv3.Client
	removeAll  bool
	newKeys    map[netip.Addr]bool
	updates    map[netip.Addr][]netip.Prefix
}

func NewCache(pathPrefix string, etcdClient *clientv3.Client) (*cache, error) {
	if etcdClient == nil {
		return nil, stacktrace.NewError("invalid <nil> etcd client")
	}
	return &cache{pathPrefix: pathPrefix, etcdClient: etcdClient, newKeys: make(map[netip.Addr]bool), updates: make(map[netip.Addr][]netip.Prefix)}, nil
}

func (c *cache) RemoveAll() error {
	c.removeAll = true
	if len(c.updates) > 0 {
		c.updates = make(map[netip.Addr][]netip.Prefix)
	}
	return nil
}

func (c *cache) load(ctx context.Context, nextHop netip.Addr) error {
	if _, exist := c.updates[nextHop]; exist {
		return nil
	}
	key := fmt.Sprintf("%s/%s", c.pathPrefix, nextHop.String())
	response, err := c.etcdClient.Get(ctx, key)
	if err != nil {
		return stacktrace.Propagate(err, "fail to retrieve value for key <%s>", key)
	}
	if len(response.Kvs) > 1 {
		return stacktrace.NewError("multiple values returned for <%s> by Get: <%#v>", key, response.Kvs)
	}
	if len(response.Kvs) == 0 {
		c.updates[nextHop] = nil
		c.newKeys[nextHop] = true
		return nil
	}
	value := response.Kvs[0].Value
	prefixes := []netip.Prefix{}
	if err := json.Unmarshal(value, &prefixes); err != nil {
		return stacktrace.Propagate(err, "fail to unmarshal <%s>", value)
	}
	c.updates[nextHop] = prefixes
	return nil
}

func (c *cache) Add(ctx context.Context, nextHop netip.Addr, prefix netip.Prefix) error {
	if err := c.load(ctx, nextHop); err != nil {
		return stacktrace.Propagate(err, "fail to load existing value for key <%s>", nextHop.String())
	}
	for _, p := range c.updates[nextHop] {
		if p == prefix {
			return nil // do not add duplicate
		}
	}
	c.updates[nextHop] = append(c.updates[nextHop], prefix)
	return nil
}

func (c *cache) Remove(ctx context.Context, nextHop netip.Addr, prefix netip.Prefix) error {
	if err := c.load(ctx, nextHop); err != nil {
		return stacktrace.Propagate(err, "fail to load existing value for key <%s>", nextHop.String())
	}
	newList := []netip.Prefix{}
	for _, p := range c.updates[nextHop] {
		if p == prefix {
			continue
		}
		newList = append(newList, p)
	}
	c.updates[nextHop] = newList
	return nil
}

func (c *cache) GetOperations() ([][]clientv3.Op, error) {
	operations := [][]clientv3.Op{}
	if c.removeAll {
		operations = append(operations, []clientv3.Op{clientv3.OpDelete(fmt.Sprintf("%s/", c.pathPrefix), clientv3.WithPrefix())})
	}
	modifications := []clientv3.Op{}
	for nextHop, prefixes := range c.updates {
		key := fmt.Sprintf("%s/%s", c.pathPrefix, nextHop.String())
		if len(prefixes) > 0 {
			binJSON, err := json.Marshal(prefixes)
			if err != nil {
				return nil, stacktrace.Propagate(err, "fail to serialize <%#v> for nextHop <%s>", prefixes, nextHop)
			}
			modifications = append(modifications, clientv3.OpPut(key, string(binJSON)))
		} else {
			if !c.newKeys[nextHop] {
				modifications = append(modifications, clientv3.OpDelete(key))
			}
		}
	}
	if len(modifications) > 0 {
		operations = append(operations, modifications)
	}
	return operations, nil
}
