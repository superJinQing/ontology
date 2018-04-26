/*
 * Copyright (C) 2018 The ontology Authors
 * This file is part of The ontology library.
 *
 * The ontology is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The ontology is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Lesser General Public License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with The ontology.  If not, see <http://www.gnu.org/licenses/>.
 */

package linkcontrol

import (
	"github.com/ontio/ontology/common/log"
	comm "github.com/ontio/ontology/p2pserver/common"
	"math/rand"
	"sync"
	"time"
)

const (
	// needAddressThreshold is the number of addresses under which the
	// address manager will claim to need more addresses.
	needAddressThreshold = 1000
	// numMissingDays is the number of days before which we assume an
	// address has vanished if we have not seen it announced  in that long.
	numMissingDays = 30
	// numRetries is the number of tried without a single success before
	// we assume an address is bad.
	numRetries = 10
)

type KnownAddress struct {
	srcAddr        comm.PeerAddr
	lastattempt    time.Time
	lastDisconnect time.Time
	attempts       int
}

type KnownAddressList struct {
	sync.RWMutex
	List      map[uint64]*KnownAddress
	addrCount uint64
}

// return the given address' last attempt time
func (ka *KnownAddress) LastAttempt() time.Time {
	return ka.lastattempt
}

// Attempt increases
func (ka *KnownAddress) increaseAttempts() {
	ka.attempts++
}

// updates the last attempt time.
func (ka *KnownAddress) updateLastAttempt() {
	// set last tried time to now
	ka.lastattempt = time.Now()
}

// update last disconnect time
func (ka *KnownAddress) updateLastDisconnect() {
	// set last disconnect time to now
	ka.lastDisconnect = time.Now()
}

// chance returns the selection probability for a known address.  The priority
// depends upon how recently the address has been seen, how recently it was last
// attempted and how often attempts to connect to it have failed.
func (ka *KnownAddress) chance() float64 {
	now := time.Now()
	lastAttempt := now.Sub(ka.lastattempt)

	if lastAttempt < 0 {
		lastAttempt = 0
	}

	c := 1.0

	// Very recent attempts are less likely to be retried.
	if lastAttempt < 10*time.Minute {
		c *= 0.01
	}

	// Failed attempts deprioritise.
	for i := ka.attempts; i > 0; i-- {
		c /= 1.5
	}

	return c
}

// isBad returns true if the address in question has not been tried in the last
// minute and meets one of the following criteria:
// 1) Just tried in one minute
// 2) It hasn't been seen in over a month
// 3) It has failed at least ten times
// 4) It has failed ten times in the last week
// All addresses that meet these criteria are assumed to be worthless and not
// worth keeping hold of.
func (ka *KnownAddress) isBad() bool {
	// just tried in one minute? This isn't suitable for very few peers.
	//if ka.lastattempt.After(time.Now().Add(-1 * time.Minute)) {
	//	return true
	//}

	// Over a month old?
	if ka.srcAddr.Time < (time.Now().Add(-1 * numMissingDays * time.Hour * 24)).UnixNano() {
		return true
	}

	// Just disconnected in one minute? This isn't suitable for very few peers.
	//if ka.lastDisconnect.After(time.Now().Add(-1 * time.Minute)) {
	//	return true
	//}

	// tried too many times?
	if ka.attempts >= numRetries {
		return true
	}

	return false
}

//save the peer's infomation
func (ka *KnownAddress) SaveAddr(p comm.PeerAddr) {
	ka.srcAddr.Time = p.Time
	ka.srcAddr.Services = p.Services
	ka.srcAddr.IpAddr = p.IpAddr
	ka.srcAddr.Port = p.Port
	ka.srcAddr.ID = p.ID
}

// return peer's address
func (ka *KnownAddress) NetAddress() comm.PeerAddr {
	return ka.srcAddr
}

// get peer's ID
func (ka *KnownAddress) GetID() uint64 {
	return ka.srcAddr.ID
}

// if need to send address message
func (al *KnownAddressList) NeedMoreAddresses() bool {
	al.Lock()
	defer al.Unlock()

	return al.addrCount < needAddressThreshold
}

// if the peer already in known address list
func (al *KnownAddressList) AddressExisted(uid uint64) bool {
	_, ok := al.List[uid]
	return ok
}

// update the known address infomation
func (al *KnownAddressList) UpdateAddress(id uint64, p comm.PeerAddr) {
	kaold := al.List[id]
	if (p.Time > kaold.srcAddr.Time) ||
		(kaold.srcAddr.Services&p.Services) !=
			p.Services {
		kaold.SaveAddr(p)
	}
}

// update peer last disconnection time
func (al *KnownAddressList) UpdateLastDisconn(id uint64) {
	ka := al.List[id]
	ka.updateLastDisconnect()
}

// add a new peer to known address list
func (al *KnownAddressList) AddAddressToKnownAddress(p comm.PeerAddr) {
	al.Lock()
	defer al.Unlock()

	ka := new(KnownAddress)
	ka.SaveAddr(p)
	if al.AddressExisted(ka.GetID()) {
		log.Debug("It is a existed addr\n")
		al.UpdateAddress(ka.GetID(), p)
	} else {
		al.List[ka.GetID()] = ka
		al.addrCount++
	}
}

// delete address for known address list
func (al *KnownAddressList) DelAddressFromList(id uint64) bool {
	al.Lock()
	defer al.Unlock()

	_, ok := al.List[id]
	if ok == false {
		return false
	}
	delete(al.List, id)
	return true
}

// get the total count of known address list
func (al *KnownAddressList) GetAddressCnt() uint64 {
	al.RLock()
	defer al.RUnlock()
	if al != nil {
		return al.addrCount
	}
	return 0
}

func (al *KnownAddressList) init() {
	al.List = make(map[uint64]*KnownAddress)
}

func isInNbrList(id uint64, nbrAddrs []comm.PeerAddr) bool {
	for _, na := range nbrAddrs {
		if id == na.ID {
			return true
		}
	}
	return false
}

// random select somme peers to connect
func (al *KnownAddressList) RandGetAddresses(nbrAddrs []comm.PeerAddr) []comm.PeerAddr {
	al.RLock()
	defer al.RUnlock()
	var keys []uint64
	for k := range al.List {
		isInNbr := isInNbrList(k, nbrAddrs)
		isBad := al.List[k].isBad()
		if isInNbr == false && isBad == false {
			keys = append(keys, k)
		}
	}

	addrLen := len(keys)
	var i int
	addrs := []comm.PeerAddr{}
	if comm.MAXOUTBOUNDCNT-len(nbrAddrs) > addrLen {
		for _, v := range keys {
			ka, ok := al.List[v]
			if !ok {
				continue
			}
			ka.increaseAttempts()
			ka.updateLastAttempt()
			addrs = append(addrs, ka.srcAddr)
		}
	} else {
		order := rand.Perm(addrLen)
		var count int
		count = comm.MAXOUTBOUNDCNT - len(nbrAddrs)
		for i = 0; i < count; i++ {
			for j, v := range keys {
				if j == order[j] {
					ka, ok := al.List[v]
					if !ok {
						continue
					}
					ka.increaseAttempts()
					ka.updateLastAttempt()
					addrs = append(addrs, ka.srcAddr)
					keys = append(keys[:j], keys[j+1:]...)
					break
				}
			}
		}
	}

	return addrs
}

// random select somme address to send to other peers
func (al *KnownAddressList) RandSelectAddresses() []comm.PeerAddr {
	al.RLock()
	defer al.RUnlock()
	var keys []uint64
	addrs := []comm.PeerAddr{}
	for k := range al.List {
		keys = append(keys, k)
	}
	addrLen := len(keys)

	var count int
	if comm.MAXOUTBOUNDCNT > addrLen {
		count = addrLen
	} else {
		count = comm.MAXOUTBOUNDCNT
	}
	for i, v := range keys {
		if i < count {
			ka, ok := al.List[v]
			if !ok {
				continue
			}
			addrs = append(addrs, ka.srcAddr)
		}
	}

	return addrs
}
