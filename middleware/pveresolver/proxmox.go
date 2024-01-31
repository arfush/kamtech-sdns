package pveresolver

import "github.com/luthermonson/go-proxmox"

type agentInterfaces struct {
	Result []proxmox.AgentNetworkIface `json:"result"`
}
