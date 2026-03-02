// Copyright 2024 New Vector Ltd.
// Copyright 2022 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/sirupsen/logrus"
)

const (
	// RelayInfoPath is the endpoint that relay-capable nodes expose.
	RelayInfoPath = "/_matrix/p2p/relay_info"

	// DefaultMaxAutoRelays is the maximum number of relays to auto-discover.
	DefaultMaxAutoRelays = 3

	// relayInfoQueryTimeout is how long to wait for a relay_info response.
	relayInfoQueryTimeout = 5 * time.Second
)

// RelayInfoResponse is returned by the relay_info endpoint.
type RelayInfoResponse struct {
	Relaying bool `json:"relaying"`
}

// RelayServerDiscovery queries discovered peers for relay capability
// and automatically registers them with the local RelayServerRetriever.
type RelayServerDiscovery struct {
	mu            sync.Mutex
	httpClient    *http.Client
	retriever     *RelayServerRetriever
	maxAutoRelays int
	autoRelays    map[spec.ServerName]struct{}
}

// NewRelayServerDiscovery creates a discovery instance that will query
// peers over the provided HTTP client (which should use the Pinecone transport)
// and register discovered relays with the retriever.
func NewRelayServerDiscovery(
	httpClient *http.Client,
	retriever *RelayServerRetriever,
) *RelayServerDiscovery {
	return &RelayServerDiscovery{
		httpClient:    httpClient,
		retriever:     retriever,
		maxAutoRelays: DefaultMaxAutoRelays,
		autoRelays:    make(map[spec.ServerName]struct{}),
	}
}

// SetMaxAutoRelays sets the maximum number of relays to auto-discover.
func (d *RelayServerDiscovery) SetMaxAutoRelays(max int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.maxAutoRelays = max
}

// OnPeerDiscovered should be called when a new peer is seen (PeerAdded or
// BroadcastReceived). It asynchronously queries the peer's relay_info
// endpoint and, if the peer is a relay, registers it.
func (d *RelayServerDiscovery) OnPeerDiscovered(peerID string) {
	serverName := spec.ServerName(peerID)

	d.mu.Lock()
	if _, already := d.autoRelays[serverName]; already {
		d.mu.Unlock()
		return
	}
	if len(d.autoRelays) >= d.maxAutoRelays {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	go func() {
		url := fmt.Sprintf("matrix://%s%s", peerID, RelayInfoPath)
		d.queryAndRegisterDirect(peerID, url)
	}()
}

// queryAndRegisterDirect queries the given URL for relay info and registers
// the peer as a relay if it reports relaying: true.
func (d *RelayServerDiscovery) queryAndRegisterDirect(peerID string, url string) {
	relaying, err := d.queryPeerDirect(url)
	if err != nil {
		logrus.WithError(err).Debugf("relay discovery: failed to query %s", peerID)
		return
	}

	if !relaying {
		return
	}

	d.registerRelay(peerID)
}

// registerRelay adds a relay server if not already known and under the limit.
func (d *RelayServerDiscovery) registerRelay(peerID string) {
	serverName := spec.ServerName(peerID)

	d.mu.Lock()
	if _, already := d.autoRelays[serverName]; already {
		d.mu.Unlock()
		return
	}
	if len(d.autoRelays) >= d.maxAutoRelays {
		d.mu.Unlock()
		logrus.Infof("relay discovery: ignoring relay %s (max %d already discovered)", peerID, d.maxAutoRelays)
		return
	}
	d.autoRelays[serverName] = struct{}{}
	d.mu.Unlock()

	logrus.Infof("relay discovery: auto-discovered relay server %s", peerID)

	// Merge with existing relay servers rather than replacing them.
	existing := d.retriever.GetRelayServers()
	for _, s := range existing {
		if s == serverName {
			return // already registered
		}
	}
	d.retriever.SetRelayServers(append(existing, serverName))
}

// queryPeerDirect queries the given URL and returns whether the peer is relaying.
func (d *RelayServerDiscovery) queryPeerDirect(url string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), relayInfoQueryTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("creating request: %w", err)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return false, fmt.Errorf("querying peer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("peer returned status %d", resp.StatusCode)
	}

	var info RelayInfoResponse
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return false, fmt.Errorf("decoding response: %w", err)
	}

	return info.Relaying, nil
}

// GetAutoDiscoveredRelays returns the set of auto-discovered relay servers.
func (d *RelayServerDiscovery) GetAutoDiscoveredRelays() []spec.ServerName {
	d.mu.Lock()
	defer d.mu.Unlock()
	relays := make([]spec.ServerName, 0, len(d.autoRelays))
	for s := range d.autoRelays {
		relays = append(relays, s)
	}
	return relays
}
