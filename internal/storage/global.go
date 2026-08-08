package storage

import "sync"

var (
	gMu   sync.RWMutex
	gSync *DataSync
	gAPK  *Client
)

func SetGlobalSync(s *DataSync) {
	gMu.Lock()
	gSync = s
	gMu.Unlock()
}

func GlobalSync() *DataSync {
	gMu.RLock()
	defer gMu.RUnlock()
	return gSync
}

func SetGlobalAPK(c *Client) {
	gMu.Lock()
	gAPK = c
	gMu.Unlock()
}

func GlobalAPK() *Client {
	gMu.RLock()
	defer gMu.RUnlock()
	return gAPK
}
