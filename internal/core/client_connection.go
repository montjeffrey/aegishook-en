package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func prepareClientConnection(o *ClientOptions, m *clientManifest) error {
	for _, name := range []string{filepath.Join("adapter", "connection.json"), "enrolled.json"} {
		b, err := os.ReadFile(filepath.Join(o.Dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		var saved ClientConnection
		if json.Unmarshal(b, &saved) != nil || saved.Endpoint != o.Endpoint || saved.Token == "" || len(saved.NodeID) != 32 {
			return errors.New("existing device connection does not match the install address; left unchanged")
		}
		if o.EnrollmentCode != "" {
			return errors.New("a device credential already exists, so no enrollment code is needed; just run the install again")
		}
		o.Token = saved.Token
		m.NodeID = saved.NodeID
		return nil
	}
	if o.Token != "" {
		return nil
	} // Internal/offline fixtures; public CLI uses enrollment only.
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("redirects are not allowed during enrollment")
	}}
	var connection ClientConnection
	if o.EnrollmentCode == "" {
		var err error
		connection, err = awaitClientApproval(client, o)
		if err != nil {
			return err
		}
	} else {
		if len(o.EnrollmentCode) != 64 {
			return errors.New("one-time enrollment code format is invalid")
		}
		res, err := client.Post(o.Endpoint+"/api/v1/client/enroll", "application/json", bytes.NewReader(mustJSON(map[string]string{"code": o.EnrollmentCode, "platform": runtime.GOOS})))
		if err != nil {
			return errors.New("cannot reach the review service; hooks not installed")
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return errors.New("enrollment failed: the code is invalid, used or expired, or the service rejected the request")
		}
		if json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&connection) != nil || len(connection.NodeID) != 32 || len(connection.Token) != 64 {
			return errors.New("the service returned an invalid device credential")
		}
	}
	connection.Endpoint = o.Endpoint
	o.Token = connection.Token
	m.NodeID = connection.NodeID
	// Keep a private retry record: a successfully consumed code must survive later filesystem errors.
	return AtomicFile(filepath.Join(o.Dir, "enrolled.json"), mustJSON(connection), 0600)
}

func awaitClientApproval(client *http.Client, o *ClientOptions) (ClientConnection, error) {
	type savedRequest struct {
		ClientRequestTicket
		Endpoint string `json:"endpoint"`
	}
	saved := savedRequest{}
	file := filepath.Join(o.Dir, "request.json")
	if b, err := os.ReadFile(file); err == nil {
		if json.Unmarshal(b, &saved) != nil || saved.Endpoint != o.Endpoint {
			return ClientConnection{}, errors.New("existing enrollment request does not match the service address")
		}
	} else if !os.IsNotExist(err) {
		return ClientConnection{}, err
	}
	if !time.Now().Before(saved.ExpiresAt) {
		name, _ := os.Hostname()
		if name == "" {
			name = runtime.GOOS + " device"
		}
		res, err := client.Post(o.Endpoint+"/api/v1/client/requests", "application/json", bytes.NewReader(mustJSON(map[string]string{"name": name, "platform": runtime.GOOS})))
		if err != nil {
			return ClientConnection{}, errors.New("cannot submit the enrollment request; check the review service address")
		}
		err = json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&saved.ClientRequestTicket)
		res.Body.Close()
		if res.StatusCode != 200 || err != nil || len(saved.ID) != 32 || len(saved.Secret) != 64 {
			return ClientConnection{}, errors.New("enrollment request failed: the service rejected it or there are too many requests")
		}
		saved.Endpoint = o.Endpoint
		if err = AtomicFile(file, mustJSON(saved), 0600); err != nil {
			return ClientConnection{}, err
		}
	}
	if o.Progress != nil {
		fmt.Fprintf(o.Progress, "Enrollment request %s\nIn the console under 'Agent integration > Remote devices', check the request ID and approve it. Waits up to ten minutes; no hook is written before approval.\n", saved.ID)
	}
	for time.Now().Before(saved.ExpiresAt) {
		req, _ := http.NewRequest("GET", o.Endpoint+"/api/v1/client/requests/"+saved.ID, nil)
		req.Header.Set("Authorization", "Bearer "+saved.Secret)
		res, err := client.Do(req)
		if err != nil {
			return ClientConnection{}, errors.New("connection lost while waiting for enrollment; run the install again to keep waiting")
		}
		var result ClientRequestResult
		err = json.NewDecoder(io.LimitReader(res.Body, 8192)).Decode(&result)
		res.Body.Close()
		if res.StatusCode != 200 || err != nil {
			return ClientConnection{}, errors.New("the service rejected the enrollment status query; check whether this origin is blocklisted")
		}
		if result.State == "approved" && result.Connection != nil && len(result.Connection.Token) == 64 && len(result.Connection.NodeID) == 32 {
			// Persist the one-time credential before deleting the application ticket.
			result.Connection.Endpoint = o.Endpoint
			if err = AtomicFile(filepath.Join(o.Dir, "enrolled.json"), mustJSON(result.Connection), 0600); err != nil {
				return ClientConnection{}, err
			}
			_ = os.Remove(file)
			return *result.Connection, nil
		}
		if result.State != "pending" {
			_ = os.Remove(file)
			return ClientConnection{}, fmt.Errorf("enrollment request ended (%s); hooks not installed; run the install again to re-request if needed", result.State)
		}
		time.Sleep(2 * time.Second)
	}
	_ = os.Remove(file)
	return ClientConnection{}, errors.New("enrollment request expired; hooks not installed")
}
