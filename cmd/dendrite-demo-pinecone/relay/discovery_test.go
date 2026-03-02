// Copyright 2024 New Vector Ltd.
// Copyright 2022 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	federationAPI "github.com/element-hq/dendrite/federationapi/api"
	relayServerAPI "github.com/element-hq/dendrite/relayapi/api"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/stretchr/testify/assert"
	"gotest.tools/v3/poll"
)

// FullFakeFedAPI supports all relay server management operations.
type FullFakeFedAPI struct {
	federationAPI.FederationInternalAPI
	mu      sync.Mutex
	servers map[spec.ServerName][]spec.ServerName
}

func NewFullFakeFedAPI() *FullFakeFedAPI {
	return &FullFakeFedAPI{
		servers: make(map[spec.ServerName][]spec.ServerName),
	}
}

func (f *FullFakeFedAPI) P2PQueryRelayServers(
	ctx context.Context,
	req *federationAPI.P2PQueryRelayServersRequest,
	res *federationAPI.P2PQueryRelayServersResponse,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	res.RelayServers = append([]spec.ServerName{}, f.servers[req.Server]...)
	return nil
}

func (f *FullFakeFedAPI) P2PAddRelayServers(
	ctx context.Context,
	req *federationAPI.P2PAddRelayServersRequest,
	res *federationAPI.P2PAddRelayServersResponse,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing := f.servers[req.Server]
	for _, newRelay := range req.RelayServers {
		found := false
		for _, s := range existing {
			if s == newRelay {
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, newRelay)
		}
	}
	f.servers[req.Server] = existing
	return nil
}

func (f *FullFakeFedAPI) P2PRemoveRelayServers(
	ctx context.Context,
	req *federationAPI.P2PRemoveRelayServersRequest,
	res *federationAPI.P2PRemoveRelayServersResponse,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	existing := f.servers[req.Server]
	var remaining []spec.ServerName
	for _, s := range existing {
		remove := false
		for _, r := range req.RelayServers {
			if s == r {
				remove = true
				break
			}
		}
		if !remove {
			remaining = append(remaining, s)
		}
	}
	f.servers[req.Server] = remaining
	return nil
}

type FullFakeRelayAPI struct {
	relayServerAPI.RelayInternalAPI
}

func (r *FullFakeRelayAPI) PerformRelayServerSync(
	ctx context.Context,
	userID spec.UserID,
	relayServer spec.ServerName,
) error {
	return nil
}

func newDiscoveryTestRetriever() *RelayServerRetriever {
	r := NewRelayServerRetriever(
		context.Background(),
		"testserver",
		NewFullFakeFedAPI(),
		&FullFakeRelayAPI{},
		make(chan bool, 1),
	)
	return &r
}

func TestQueryPeerRelayInfo_Relaying(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, RelayInfoPath, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RelayInfoResponse{Relaying: true})
	}))
	defer srv.Close()

	discovery := NewRelayServerDiscovery(srv.Client(), newDiscoveryTestRetriever())

	relaying, err := discovery.queryPeerDirect(srv.URL + RelayInfoPath)
	assert.NoError(t, err)
	assert.True(t, relaying)
}

func TestQueryPeerRelayInfo_NotRelaying(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RelayInfoResponse{Relaying: false})
	}))
	defer srv.Close()

	discovery := NewRelayServerDiscovery(srv.Client(), newDiscoveryTestRetriever())

	relaying, err := discovery.queryPeerDirect(srv.URL + RelayInfoPath)
	assert.NoError(t, err)
	assert.False(t, relaying)
}

func TestQueryPeerRelayInfo_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	discovery := NewRelayServerDiscovery(srv.Client(), newDiscoveryTestRetriever())

	_, err := discovery.queryPeerDirect(srv.URL + RelayInfoPath)
	assert.Error(t, err)
}

func TestDiscoveryMaxRelays(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RelayInfoResponse{Relaying: true})
	}))
	defer srv.Close()

	retriever := newDiscoveryTestRetriever()
	discovery := NewRelayServerDiscovery(srv.Client(), retriever)
	discovery.SetMaxAutoRelays(2)

	discovery.queryAndRegisterDirect("relay1", srv.URL+RelayInfoPath)
	discovery.queryAndRegisterDirect("relay2", srv.URL+RelayInfoPath)
	discovery.queryAndRegisterDirect("relay3", srv.URL+RelayInfoPath)

	relays := discovery.GetAutoDiscoveredRelays()
	assert.Equal(t, 2, len(relays))
}

func TestDiscoveryNoDuplicates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RelayInfoResponse{Relaying: true})
	}))
	defer srv.Close()

	retriever := newDiscoveryTestRetriever()
	discovery := NewRelayServerDiscovery(srv.Client(), retriever)

	discovery.queryAndRegisterDirect("relay1", srv.URL+RelayInfoPath)
	discovery.queryAndRegisterDirect("relay1", srv.URL+RelayInfoPath)

	relays := discovery.GetAutoDiscoveredRelays()
	assert.Equal(t, 1, len(relays))
}

func TestDiscoverySkipsNonRelays(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RelayInfoResponse{Relaying: false})
	}))
	defer srv.Close()

	retriever := newDiscoveryTestRetriever()
	discovery := NewRelayServerDiscovery(srv.Client(), retriever)

	discovery.queryAndRegisterDirect("peer1", srv.URL+RelayInfoPath)

	relays := discovery.GetAutoDiscoveredRelays()
	assert.Equal(t, 0, len(relays))
}

func TestDiscoveryRegistersWithRetriever(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(RelayInfoResponse{Relaying: true})
	}))
	defer srv.Close()

	retriever := newDiscoveryTestRetriever()
	discovery := NewRelayServerDiscovery(srv.Client(), retriever)

	discovery.queryAndRegisterDirect("relay1", srv.URL+RelayInfoPath)

	// The retriever should now have the relay registered
	check := func(log poll.LogT) poll.Result {
		servers := retriever.GetRelayServers()
		for _, s := range servers {
			if string(s) == "relay1" {
				return poll.Success()
			}
		}
		return poll.Continue("waiting for relay to be registered")
	}
	poll.WaitOn(t, check, poll.WithTimeout(2*time.Second), poll.WithDelay(50*time.Millisecond))
}
