package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/encli-own/encli/pkg/crypto"
)

const directoryVersion = "encli-directory-v1"

type DirectoryEntry struct {
	BlindedID string `json:"blinded_id"`
	Profile   string `json:"profile"`
	Timestamp int64  `json:"timestamp"`
}

type DirectoryStore struct {
	mu      sync.RWMutex
	entries map[string]*DirectoryEntry
}

func NewDirectoryStore() *DirectoryStore {
	return &DirectoryStore{
		entries: make(map[string]*DirectoryEntry),
	}
}

func computeBlindedID(nickname string) string {
	normalized := strings.ToLower(strings.TrimSpace(nickname))
	mac := hmac.New(sha256.New, []byte(directoryVersion))
	mac.Write([]byte(normalized))
	return hex.EncodeToString(mac.Sum(nil))
}

func (ds *DirectoryStore) Publish(nickname, profileData string) (*DirectoryEntry, error) {
	blindedID := computeBlindedID(nickname)
	entry := &DirectoryEntry{
		BlindedID: blindedID,
		Profile:   profileData,
		Timestamp: time.Now().Unix(),
	}
	ds.mu.Lock()
	ds.entries[blindedID] = entry
	ds.mu.Unlock()
	return entry, nil
}

func (ds *DirectoryStore) Lookup(blindedID string) *DirectoryEntry {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return ds.entries[blindedID]
}

func (ds *DirectoryStore) Search(blindedIDPrefix string) []*DirectoryEntry {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	var results []*DirectoryEntry
	for _, entry := range ds.entries {
		if strings.HasPrefix(entry.BlindedID, blindedIDPrefix) {
			results = append(results, entry)
		}
	}
	return results
}

func (ds *DirectoryStore) Delete(blindedID, sig string) error {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if _, ok := ds.entries[blindedID]; !ok {
		return fmt.Errorf("entry not found")
	}
	delete(ds.entries, blindedID)
	return nil
}

func encryptProfile(nickname, deviceID string) (string, error) {
	key := deriveEncKey(nickname)
	ciph, err := crypto.NewCipher(key)
	if err != nil {
		return "", err
	}
	payload := fmt.Sprintf(`{"nickname":"%s","device_id":"%s"}`, nickname, deviceID)
	enc, err := ciph.Encrypt([]byte(payload), nil)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(enc), nil
}

func decryptProfile(nickname, encryptedHex string) (map[string]string, error) {
	key := deriveEncKey(nickname)
	ciph, err := crypto.NewCipher(key)
	if err != nil {
		return nil, err
	}
	enc, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return nil, err
	}
	data, err := ciph.Decrypt(enc, nil)
	if err != nil {
		return nil, err
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result["nickname"] != nickname {
		return nil, fmt.Errorf("nickname mismatch")
	}
	return result, nil
}

func deriveEncKey(nickname string) []byte {
	normalized := strings.ToLower(strings.TrimSpace(nickname))
	h := sha256.Sum256([]byte("encli-dir-enc:" + normalized))
	return h[:]
}
