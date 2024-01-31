package pveresolver

import (
	"context"
	"fmt"
	"github.com/luthermonson/go-proxmox"
	"github.com/miekg/dns"
	"github.com/semihalev/log"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

type cache struct {
	mtx sync.RWMutex
	c   map[string]net.IP

	pve *proxmox.Client
	net *net.IPNet

	crashed atomic.Bool
}

func newCache(pve *proxmox.Client, network *net.IPNet) *cache {
	return &cache{
		c:       make(map[string]net.IP),
		pve:     pve,
		net:     network,
		crashed: atomic.Bool{},
	}
}

func (c *cache) goCaching() {
	go func() {
		cluster, err := c.pve.Cluster(context.Background())
		if err != nil {
			log.Error("unable to get proxmox cluster", "err", err)
			c.crashed.Store(true)
			return
		}

		for {
			rs, err := cluster.Resources(context.Background(), "vm")
			if err != nil {
				log.Error("unable to get proxmox vm list", "err", err)
				c.crashed.Store(true)
				return
			}

			for _, r := range rs {
				switch r.Type {
				case "qemu":
					c.goUpdateQemu(r)
				}
			}

			time.Sleep(time.Second * 30)
		}
	}()
}

func (c *cache) get(fqnd string) (ip net.IP, ok bool) {
	if c.crashed.Load() {
		log.Error("proxmox cache crashed. unable handle request")
		return nil, false
	}

	c.mtx.RLock()
	defer c.mtx.RUnlock()

	ip, ok = c.c[dns.Fqdn(fqnd)]
	return
}

func (c *cache) goUpdateQemu(r *proxmox.ClusterResource) {
	go func() {
		fqnd := dns.Fqdn(r.Name)
		var ifaces agentInterfaces
		err := c.pve.Get(context.Background(), fmt.Sprintf("/nodes/%s/qemu/%d/agent/network-get-interfaces", r.Node, r.VMID), &ifaces)
		if err != nil {
			log.Error("unable to get vm interfaces", "err", err)
			return
		}

		for _, iface := range ifaces.Result {
			for _, addr := range iface.IPAddresses {
				if addr.IPAddressType != "ipv4" {
					continue
				}
				ip := net.ParseIP(addr.IPAddress)
				if c.net.Contains(ip) && !c.c[fqnd].Equal(ip) {
					c.mtx.Lock()
					c.c[fqnd] = ip
					log.Info("updated a record", "vm", r.Name, "ip", addr.IPAddress)
					c.mtx.Unlock()
				}
			}
		}
	}()
}
