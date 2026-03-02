// Copyright 2024 New Vector Ltd.
// Copyright 2022 The Matrix.org Foundation C.I.C.
//
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Element-Commercial
// Please see LICENSE files in the repository root for full details.

package relay

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	federationAPI "github.com/element-hq/dendrite/federationapi/api"
	relayServerAPI "github.com/element-hq/dendrite/relayapi/api"
	"github.com/matrix-org/gomatrixserverlib/spec"
	"github.com/sirupsen/logrus"
)

const (
	relayServerRetryInterval = time.Second * 30
	// After all relays are synced, periodically re-check them in case new
	// messages arrived after the initial sync completed.
	relaySyncedRecheckInterval = time.Minute * 5
)

type RelayServerRetriever struct {
	ctx                 context.Context
	serverName          spec.ServerName
	federationAPI       federationAPI.FederationInternalAPI
	relayAPI            relayServerAPI.RelayInternalAPI
	relayServersQueried map[spec.ServerName]bool
	queriedServersMutex sync.Mutex
	running             atomic.Bool
	quit                chan bool
}

func NewRelayServerRetriever(
	ctx context.Context,
	serverName spec.ServerName,
	federationAPI federationAPI.FederationInternalAPI,
	relayAPI relayServerAPI.RelayInternalAPI,
	quit chan bool,
) RelayServerRetriever {
	return RelayServerRetriever{
		ctx:                 ctx,
		serverName:          serverName,
		federationAPI:       federationAPI,
		relayAPI:            relayAPI,
		relayServersQueried: make(map[spec.ServerName]bool),
		running:             atomic.Bool{},
		quit:                quit,
	}
}

func (r *RelayServerRetriever) InitializeRelayServers(eLog *logrus.Entry) {
	request := federationAPI.P2PQueryRelayServersRequest{Server: spec.ServerName(r.serverName)}
	response := federationAPI.P2PQueryRelayServersResponse{}
	err := r.federationAPI.P2PQueryRelayServers(r.ctx, &request, &response)
	if err != nil {
		eLog.Warnf("Failed obtaining list of this node's relay servers: %s", err.Error())
	}

	r.queriedServersMutex.Lock()
	defer r.queriedServersMutex.Unlock()
	for _, server := range response.RelayServers {
		r.relayServersQueried[server] = false
	}

	eLog.Infof("Registered relay servers: %v", response.RelayServers)
}

func (r *RelayServerRetriever) SetRelayServers(servers []spec.ServerName) {
	UpdateNodeRelayServers(r.serverName, servers, r.ctx, r.federationAPI)

	// Replace list of servers to sync with and mark them all as unsynced.
	r.queriedServersMutex.Lock()
	defer r.queriedServersMutex.Unlock()
	r.relayServersQueried = make(map[spec.ServerName]bool)
	for _, server := range servers {
		r.relayServersQueried[server] = false
	}

	r.StartSync()
}

func (r *RelayServerRetriever) GetRelayServers() []spec.ServerName {
	r.queriedServersMutex.Lock()
	defer r.queriedServersMutex.Unlock()
	relayServers := []spec.ServerName{}
	for server := range r.relayServersQueried {
		relayServers = append(relayServers, server)
	}

	return relayServers
}

func (r *RelayServerRetriever) GetQueriedServerStatus() map[spec.ServerName]bool {
	r.queriedServersMutex.Lock()
	defer r.queriedServersMutex.Unlock()

	result := map[spec.ServerName]bool{}
	for server, queried := range r.relayServersQueried {
		result[server] = queried
	}
	return result
}

func (r *RelayServerRetriever) StartSync() {
	if !r.running.Load() {
		logrus.Info("Starting relay server sync")
		go r.SyncRelayServers(r.quit)
	}
}

func (r *RelayServerRetriever) IsRunning() bool {
	return r.running.Load()
}

func (r *RelayServerRetriever) SyncRelayServers(stop <-chan bool) {
	defer r.running.Store(false)

	t := time.NewTimer(relayServerRetryInterval)
	for {
		relayServersToQuery := []spec.ServerName{}
		allSynced := true
		func() {
			r.queriedServersMutex.Lock()
			defer r.queriedServersMutex.Unlock()
			for server, complete := range r.relayServersQueried {
				if !complete {
					relayServersToQuery = append(relayServersToQuery, server)
					allSynced = false
				}
			}
		}()

		if allSynced && len(relayServersToQuery) == 0 {
			// All relay servers have been synced. Re-mark them as unsynced
			// and re-check after a longer interval to catch messages that
			// arrived at the relay after our initial sync completed.
			logrus.Info("All relays synced; scheduling periodic re-check")
			func() {
				r.queriedServersMutex.Lock()
				defer r.queriedServersMutex.Unlock()
				for server := range r.relayServersQueried {
					r.relayServersQueried[server] = false
				}
			}()
			t.Reset(relaySyncedRecheckInterval)
		} else {
			r.queryRelayServers(relayServersToQuery)
			t.Reset(relayServerRetryInterval)
		}

		select {
		case <-stop:
			if !t.Stop() {
				<-t.C
			}
			logrus.Info("Stopped relay server retriever")
			return
		case <-t.C:
		}
	}
}

func (r *RelayServerRetriever) queryRelayServers(relayServers []spec.ServerName) {
	logrus.Info("Querying relay servers for any available transactions")
	for _, server := range relayServers {
		userID, err := spec.NewUserID("@user:"+string(r.serverName), false)
		if err != nil {
			return
		}

		logrus.Infof("Syncing with relay: %s", string(server))
		err = r.relayAPI.PerformRelayServerSync(context.Background(), *userID, server)
		if err == nil {
			func() {
				r.queriedServersMutex.Lock()
				defer r.queriedServersMutex.Unlock()
				r.relayServersQueried[server] = true
			}()
			// NOTE: The relay may receive new messages after this sync completes.
			// SyncRelayServers handles this by periodically re-marking all relays
			// as unsynced and re-checking them after relaySyncedRecheckInterval.
		} else {
			logrus.Errorf("Failed querying relay server: %s", err.Error())
		}
	}
}

func UpdateNodeRelayServers(
	node spec.ServerName,
	relays []spec.ServerName,
	ctx context.Context,
	fedAPI federationAPI.FederationInternalAPI,
) {
	// Get the current relay list
	request := federationAPI.P2PQueryRelayServersRequest{Server: node}
	response := federationAPI.P2PQueryRelayServersResponse{}
	err := fedAPI.P2PQueryRelayServers(ctx, &request, &response)
	if err != nil {
		logrus.Warnf("Failed obtaining list of relay servers for %s: %s", node, err.Error())
	}

	// Remove old, non-matching relays
	var serversToRemove []spec.ServerName
	for _, existingServer := range response.RelayServers {
		shouldRemove := true
		for _, newServer := range relays {
			if newServer == existingServer {
				shouldRemove = false
				break
			}
		}

		if shouldRemove {
			serversToRemove = append(serversToRemove, existingServer)
		}
	}
	removeRequest := federationAPI.P2PRemoveRelayServersRequest{
		Server:       node,
		RelayServers: serversToRemove,
	}
	removeResponse := federationAPI.P2PRemoveRelayServersResponse{}
	err = fedAPI.P2PRemoveRelayServers(ctx, &removeRequest, &removeResponse)
	if err != nil {
		logrus.Warnf("Failed removing old relay servers for %s: %s", node, err.Error())
	}

	// Add new relays
	addRequest := federationAPI.P2PAddRelayServersRequest{
		Server:       node,
		RelayServers: relays,
	}
	addResponse := federationAPI.P2PAddRelayServersResponse{}
	err = fedAPI.P2PAddRelayServers(ctx, &addRequest, &addResponse)
	if err != nil {
		logrus.Warnf("Failed adding relay servers for %s: %s", node, err.Error())
	}
}
