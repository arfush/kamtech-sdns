package pveresolver

import (
	"context"
	"crypto/tls"
	"github.com/luthermonson/go-proxmox"
	"github.com/miekg/dns"
	"github.com/semihalev/log"
	"github.com/semihalev/sdns/config"
	"github.com/semihalev/sdns/middleware"
	"net"
	"net/http"
)

var name = "pveresolver"

type PVEResolver struct {
	c *cache
}

func New(cfg *config.Config) *PVEResolver {
	pve := proxmox.NewClient(cfg.PVEResolver.Endpoint, proxmox.WithAPIToken(cfg.PVEResolver.Token, cfg.PVEResolver.Secret), proxmox.WithHTTPClient(&http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}))
	_, err := pve.Version(context.Background())
	if err != nil {
		log.Crit("authentication failure", "err", err)
	}

	_, network, err := net.ParseCIDR(cfg.PVEResolver.Network)
	if err != nil {
		log.Crit("unable parse pveresolver.network", "err", err)
	}
	c := newCache(pve, network)
	c.goCaching()

	return &PVEResolver{
		c: c,
	}
}

func (m *PVEResolver) Name() string {
	return name
}

func (m *PVEResolver) ServeDNS(ctx context.Context, chain *middleware.Chain) {
	req := chain.Request
	if req.Question[0].Qtype != dns.TypeA {
		chain.Next(ctx)
		return
	}

	ip, ok := m.c.get(req.Question[0].Name)
	if !ok {
		chain.Next(ctx)
		return
	}

	res := new(dns.Msg)
	res.SetReply(req)
	res.Authoritative = false
	res.Answer = []dns.RR{
		&dns.A{
			Hdr: dns.RR_Header{
				Name:   req.Question[0].Name,
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    60,
			},
			A: ip,
		},
	}
	_ = chain.Writer.WriteMsg(res)
	chain.Cancel()
}
