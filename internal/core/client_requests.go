package core

import (
	"errors"
	"net/netip"
	"time"
)

type ClientRequest struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Platform  string    `json:"platform"`
	IP        string    `json:"ip"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type storedClientRequest struct {
	ClientRequest
	SecretHash string `json:"secretHash"`
}
type ClientRequestTicket struct {
	ID        string    `json:"id"`
	Secret    string    `json:"secret"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type ClientRequestResult struct {
	State      string            `json:"state"`
	Connection *ClientConnection `json:"connection,omitempty"`
}
type ClientIPBlock struct {
	ID        string    `json:"id"`
	IP        string    `json:"ip"`
	CreatedAt time.Time `json:"createdAt"`
}

func (e *Engine) ClientIPBlocked(ip string) bool {
	var b ClientIPBlock
	return e.get("client_ip_blocks", credentialHash(ip), &b) == nil
}
func (e *Engine) ClientIPBlocks() ([]ClientIPBlock, error) {
	return list[ClientIPBlock](e, "client_ip_blocks")
}
func (e *Engine) UnblockClientIP(id string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.change("client_ip_blocks", id, nil, "client.ip.unblock", "removed source IP from the blocklist")
}
func (e *Engine) ClientRequests() ([]ClientRequest, error) {
	records, err := list[storedClientRequest](e, "client_requests")
	out := []ClientRequest{}
	for _, r := range records {
		if (r.State == "pending" || r.State == "approved") && !e.clock().Before(r.ExpiresAt) {
			r.State = "expired"
		}
		out = append(out, r.ClientRequest)
	}
	return out, err
}
func (e *Engine) CreateClientRequest(name, platform, ip string) (ClientRequestTicket, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(name) == 0 || len(name) > 120 || !hasExact(platform, "linux", "darwin", "windows") {
		return ClientRequestTicket{}, errors.New("device name or platform is invalid")
	}
	if _, err := netip.ParseAddr(ip); err != nil {
		return ClientRequestTicket{}, errors.New("cannot identify the request origin")
	}
	if e.ClientIPBlocked(ip) {
		return ClientRequestTicket{}, errors.New("this origin is blocked from connecting")
	}
	records, err := e.ClientRequests()
	if err != nil {
		return ClientRequestTicket{}, err
	}
	count, total := 0, 0
	for _, r := range records {
		if e.clock().Before(r.ExpiresAt) {
			total++
			if r.IP == ip {
				count++
			}
		}
	}
	if count >= 5 || total >= 256 {
		return ClientRequestTicket{}, errors.New("too many enrollment requests; try again later")
	}
	id, secret := ID(), ID()+ID()
	expires := e.clock().Add(10 * time.Minute)
	r := storedClientRequest{ClientRequest: ClientRequest{ID: id, Name: name, Platform: platform, IP: ip, State: "pending", CreatedAt: e.clock(), ExpiresAt: expires}, SecretHash: credentialHash(secret)}
	if err = e.change("client_requests", id, r, "client.request", "device enrollment request received"); err != nil {
		return ClientRequestTicket{}, err
	}
	return ClientRequestTicket{ID: id, Secret: secret, ExpiresAt: expires}, nil
}
func (e *Engine) DecideClientRequest(id, decision string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	var r storedClientRequest
	if err := e.get("client_requests", id, &r); err != nil {
		return errors.New("enrollment request does not exist")
	}
	if r.State != "pending" || !e.clock().Before(r.ExpiresAt) {
		return errors.New("request was already handled or has expired")
	}
	switch decision {
	case "approve":
		if e.ClientIPBlocked(r.IP) {
			return errors.New("this origin is blocklisted; remove it from the blocklist first")
		}
		r.State = "approved"
	case "reject":
		r.State = "rejected"
	case "block_ip":
		block := ClientIPBlock{ID: credentialHash(r.IP), IP: r.IP, CreatedAt: e.clock()}
		if err := e.change("client_ip_blocks", block.ID, block, "client.ip.block", "blocked source IP from connecting"); err != nil {
			return err
		}
		r.State = "rejected"
	default:
		return errors.New("only approve, reject and block_ip are supported")
	}
	return e.change("client_requests", id, r, "client.request."+decision, "handled device enrollment request")
}
func (e *Engine) PollClientRequest(id, secret, ip string) (ClientRequestResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	var r storedClientRequest
	if len(secret) != 64 || e.get("client_requests", id, &r) != nil || credentialHash(secret) != r.SecretHash {
		return ClientRequestResult{}, errors.New("request credential is invalid")
	}
	if e.ClientIPBlocked(ip) || e.ClientIPBlocked(r.IP) {
		return ClientRequestResult{}, errors.New("this origin is blocked from connecting")
	}
	if !e.clock().Before(r.ExpiresAt) {
		return ClientRequestResult{State: "expired"}, nil
	}
	if r.State != "approved" {
		return ClientRequestResult{State: r.State}, nil
	}
	r.State = "consumed"
	connection, err := e.issueClientCredential("client_requests", r.ID, r, r.Name, r.Platform)
	if err != nil {
		return ClientRequestResult{}, err
	}
	return ClientRequestResult{State: "approved", Connection: &connection}, nil
}
