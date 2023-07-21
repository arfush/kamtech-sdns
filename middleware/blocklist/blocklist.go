package blocklist

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"

	"github.com/miekg/dns"
	"github.com/semihalev/sdns/config"
	"github.com/semihalev/sdns/middleware"
)

// BlockList type
type BlockList struct {
	mu sync.RWMutex

	nullroute  net.IP
	null6route net.IP

	m map[string]bool
	w map[string]bool

	cfg *config.Config
}

func init() {
	middleware.Register(name, func(cfg *config.Config) middleware.Handler {
		return New(cfg)
	})
}

// New returns a new BlockList
func New(cfg *config.Config) *BlockList {
	b := &BlockList{
		nullroute:  net.ParseIP(cfg.Nullroute),
		null6route: net.ParseIP(cfg.Nullroutev6),

		m: make(map[string]bool),
		w: make(map[string]bool),

		cfg: cfg,
	}

	go b.fetchBlocklists()

	return b
}

// Name return middleware name
func (b *BlockList) Name() string { return name }

// ServeDNS implements the Handle interface.
func (b *BlockList) ServeDNS(ctx context.Context, ch *middleware.Chain) {
	w, req := ch.Writer, ch.Request

	q := req.Question[0]

	if !b.Exists(q.Name) {
		ch.Next(ctx)
		return
	}

	msg := new(dns.Msg)
	msg.SetReply(req)
	msg.Authoritative, msg.RecursionAvailable = true, true

	switch q.Qtype {
	case dns.TypeA:
		rrHeader := dns.RR_Header{
			Name:   q.Name,
			Rrtype: dns.TypeA,
			Class:  dns.ClassINET,
			Ttl:    3600,
		}
		a := &dns.A{Hdr: rrHeader, A: b.nullroute}
		msg.Answer = append(msg.Answer, a)
	case dns.TypeAAAA:
		rrHeader := dns.RR_Header{
			Name:   q.Name,
			Rrtype: dns.TypeAAAA,
			Class:  dns.ClassINET,
			Ttl:    3600,
		}
		a := &dns.AAAA{Hdr: rrHeader, AAAA: b.null6route}
		msg.Answer = append(msg.Answer, a)
	default:
		rrHeader := dns.RR_Header{
			Name:   q.Name,
			Rrtype: dns.TypeSOA,
			Class:  dns.ClassINET,
			Ttl:    86400,
		}
		soa := &dns.SOA{
			Hdr:     rrHeader,
			Ns:      q.Name,
			Mbox:    ".",
			Serial:  0,
			Refresh: 28800,
			Retry:   7200,
			Expire:  604800,
			Minttl:  86400,
		}
		msg.Extra = append(msg.Answer, soa)
	}

	_ = w.WriteMsg(msg)

	ch.Cancel()
}

// Get returns the entry for a key or an error
func (b *BlockList) Get(key string) (bool, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	key = dns.CanonicalName(key)
	val, ok := b.m[key]

	if !ok {
		return false, errors.New("block not found")
	}

	return val, nil
}

// Remove removes an entry from the cache
func (b *BlockList) Remove(key string) bool {
	if !b.Exists(key) {
		return false
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	key = dns.CanonicalName(key)
	delete(b.m, key)
	b.save()

	return true
}

// Set sets a value in the BlockList
func (b *BlockList) Set(key string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	key = dns.CanonicalName(key)

	if b.w[key] {
		return false
	}

	b.m[key] = true
	b.save()

	return true
}

// Exists returns whether or not a key exists in the cache
func (b *BlockList) Exists(key string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	key = dns.CanonicalName(key)
	_, ok := b.m[key]

	return ok
}

// Length returns the caches length
func (b *BlockList) Length() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	return len(b.m)
}

func (b *BlockList) save() {
	path := filepath.Join(b.cfg.BlockListDir, "local")

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return
	}

	_, _ = file.WriteString("# The file generated by auto. DO NOT EDIT\n")
	for d := range b.m {
		_, _ = file.WriteString(d + "\n")
	}

	_ = file.Close()
}

const name = "blocklist"
