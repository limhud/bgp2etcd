#!/bin/bash                                                                    
systemctl stop bgp2etcd || true
systemctl disable bgp2etcd || true
